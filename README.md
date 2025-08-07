# Learning about containers

## Motivations
Recently I've been learning about Docker and how to use it. Though I understand what it achieves on the surface, how it worked under the hood was completely unknown to me. So in order to help me understand Docker (and containers in general) at a low level, I decided that I would build a container from scratch.

## Project walkthrough
This is a rootless container that I can use to run a bash process that interacts with an Alpine Linux filesystem.

![](/images/{159DC8A9-ED87-4668-8D49-B75796CD6E0E}.png)

### Rootless containers and namespaces
A rootless container is basically a container that has elevated privileges only within its namespace. On the host, the namespace is just another non-root user. To achieve a rootless container, I had to clone a new user as well as a new namespace, in Go code, it would look like this:
```
cmd.SysProcAttr := &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS,
    UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
	},
	GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
    },

	Credential: &syscall.Credential{
			Uid: 0,
			Gid: 0,
	},
}
```

After setting clone flags, the uid and gid mappings here map the container ID, which is root in the namespace, to the current user and group IDs on host, which are non-root on host. The credential just tells the container to run as root (in the namespace).

So why a rootless container? A rootless container would prevent the processes within from affecting files on host. It would also allow us to chroot since we are now a root user inside our container.

### chroot
Change root, or chroot, allows us to limit the scope of the filesystem that our container has access to. In Go, this is done by:
```
if err := syscall.Chroot("/home/jdubs/alpine-fs"); err != nil {
		return err
}
if err := syscall.Chdir("/"); err != nil {
		return err
}
```

For this project, I just curled an Alpine Linux filesystem and unzipped it into an `alpine-fs` directory. When we pull down a Docker image and run it for example, conceptually, Docker is doing the same thing as what I did with `alpine-fs`. It's "curling" everything needed to run the application, unzipping it into a directory, then chrooting to that unzipped directory.

That's why when we run containers, we only have access to a minimal filesystem needed to run the application, nothing more.

![](/images/{267BF884-5517-4166-8BF7-83091B42F45D}.png)

### Unshare flags and mounting
Mounting involves making a resource accessible via a path in the filesystem. In this project, I used mounting to isolate process IDs in the container from ones on the host and vice versa.
```
if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return err
}
```

This code snippet allows me to view only process IDs within the container when I run `ps` within the container. However, there is a problem with this, while running the container, when I run `ps` on host, it still shows me processes happening within the container together with all other processes running on host. I don't want that, so this is where unshare flags come into play.

Unshare flags basically prevent changes in the container from propagating into the host's filesystem.
```
Unshareflags: syscall_CLONE_NEWNS,
```

By adding this line to `&syscall.SysProcAttr{}`, process IDs within the container don't show when I run `ps` on host:

![](/images/{4B04F37F-C543-4878-B690-2D00CB6C6271}.png)

Unshare flags and mounts are particularly useful as not only does it isolate what's happening within the container, it also allows me to see the parameters I set for cgroups from within the container.

### cgroups
If chroot changes what the container can see, then cgroups control how much a container can use. Control groups or cgroups allow us to limit how much resources, like memory and cpu a container can use. They also allow us to limit the number of processes a container can have running.

Setting up cgroups for my container was a major hurdle. Firstly, my Linux distro was running cgroup v1, meaning I had to create a hierarchy for each resource I would like to limit. That wouldn't be a problem, I could just do it through Go code, just use `os.Mkdir()` in the `/sys/fs/cgroup/` directory for each cgroup and call it a day...But I was building a rootless container. While in the container, I wasn't able to alter anything in the `/sys/fs/cgroup/` directory, remember, I only have root privileges within my container, not on host.

Therefore, this meant that I had to delegate cgroup subtrees for each cgroup resource, this would allow me to write limits on resources from within the container.

To achieve this, I first created directories within each cgroup resource I wanted to limit, take memory for example:
```
cd /sys/fs/cgroup/memory
sudo mkdir container
```

I then changed the owner of this `container` directory to me (the user):
```
chown -R jdubs:jdubs container
```

This recursively changes the owner of the directory and all files in the directory to the user on host. I made sure to use the user that I mapped the container ID to.

I then changed the write permissions of the files I needed to write to:
```
cd container
chmod a+w memory.limit_in_bytes cgroup.procs notify_on_release
```

Done, I've delegated a cgroup subtree that my container now can use. I then repeated the steps for cpu as well as pids. All that's left to do is write the limits on resources to these delegated subtrees. Here is the code I wrote for it:
```
pids := "/sys/fs/cgroup/pids/container"
mem := "/sys/fs/cgroup/memory/container"
cpu := "/sys/fs/cgroup/cpu/container"

if err := os.WriteFile(path.Join(pids, "pids.max"), []byte("30"), 0777); err != nil {
	return err
}
if err := os.WriteFile(path.Join(mem, "memory.limit_in_bytes"), []byte("31457280"), 0777); err != nil {
	return err
}
if err := os.WriteFile(path.Join(cpu, "cpu.cfs_quota_us"), []byte("50000"), 0777); err != nil {
	return err
}

cGroups := []string{pids, mem, cpu}
for _, group := range cGroups {
	if err := os.WriteFile(path.Join(group, "notify_on_release"), []byte("1"), 0777); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(group, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0777); err != nil {
		return err
	}
}
```

The write to `notify_on_release` makes it such that the cgroup is removed when the container exits while the write to `cgroup.procs` places the container's processes within `cgroup.procs`, putting the limits on these processes.

Of course, to be able to see these limits I've set from within my container, I would then have to mount the files in `alpine-fs` to my delegated cgroup subtrees. Here's the code snippet that I used to achieve this:
```
pidsTarget := "/home/jdubs/alpine-fs/sys/fs/cgroup/pids"
memTarget := "/home/jdubs/alpine-fs/sys/fs/cgroup/memory"
cpuTarget := "/home/jdubs/alpine-fs/sys/fs/cgroup/cpu"

targets := []string{pidsTarget, memTarget, cpuTarget}
for i := range targets {
	if err := syscall.Mount(cGroups[i], targets[i], "", syscall.MS_BIND, ""); err != nil {
		return err
	}
}
```
Here's the end result after mounting, I can now see the resource limits from within the container:

![](/images/{080248C6-B0D9-454C-B1C0-2F9E85990078}.png)

And with that, I've finished building a simple rootless container. Do note that I didn't cover everything I did in this readme, check out the [source code](https://github.com/junwei890/container/blob/main/main.go) if you're interested in how I put everything together.

## Fin
After completing this project, I had a better understanding of how containers work, I also had a deeper understanding of the inner workings of the Linux operating system (its filesystem, privilege and resource management).

This project has sparked newfound interests that I would like to explore further, like what exactly are syscalls? This could be something I learn about in a future project, maybe writing a kernel from scratch?
