package main

import (
	"log"
	"os"
	"os/exec"
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
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWUTS,

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

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func sub() error {
	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	syscall.Sethostname([]byte("container"))

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
