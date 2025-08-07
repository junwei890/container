package main

import (
	"log"
	"os"
	"os/exec"
	"path"
	"strconv"
	"syscall"
)

func main() {
	switch os.Args[1] {
	case "run":
		if err := run(); err != nil {
			log.Fatal(err)
		}
	case "sub":
		if err := sub(); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("invalid command")
	}
}

func run() error {
	cmd := exec.Command("/proc/self/exe", append([]string{"sub"}, os.Args[2:]...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUSER | syscall.CLONE_NEWUTS | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID,
		Unshareflags: syscall.CLONE_NEWNS,

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

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func sub() error {
	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cGroups(); err != nil {
		return err
	}

	if err := syscall.Sethostname([]byte("container")); err != nil {
		return err
	}

	if err := syscall.Chroot("/home/jdubs/alpine-fs"); err != nil {
		return err
	}
	if err := syscall.Chdir("/"); err != nil {
		return err
	}

	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return err
	}

	log.Printf("running container as proc %d", os.Getpid())

	if err := cmd.Run(); err != nil {
		return err
	}

	if err := syscall.Unmount("/proc", 0); err != nil {
		return err
	}
	cGroups := []string{"pids", "memory", "cpu"}
	for _, group := range cGroups {
		if err := syscall.Unmount(path.Join("/sys/fs/cgroup", group), 0); err != nil {
			return err
		}
	}

	return nil
}

func cGroups() error {
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

	pidsTarget := "/home/jdubs/alpine-fs/sys/fs/cgroup/pids"
	memTarget := "/home/jdubs/alpine-fs/sys/fs/cgroup/memory"
	cpuTarget := "/home/jdubs/alpine-fs/sys/fs/cgroup/cpu"

	targets := []string{pidsTarget, memTarget, cpuTarget}
	for i := range targets {
		if err := syscall.Mount(cGroups[i], targets[i], "", syscall.MS_BIND, ""); err != nil {
			return err
		}
	}

	return nil
}
