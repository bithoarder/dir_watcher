package main

import (
	"flag"
	"fmt"
	"github.com/howeyc/fsnotify"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var buildCmdFlag = flag.String("build", "", "command used make the project, this will not be interrupted when a file is changed.")
var runCmdFlag = flag.String("run", "", "command used run the project, only run if build suceeds.")
var runDelayFlag = flag.Duration("delay", 1*time.Second, "duration to wait after file change, before restarting command.")

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s: [flags...] <dir> [<dir>...]\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	if len(flag.Args()) == 0 || (*buildCmdFlag == "" && *runCmdFlag == "") {
		flag.Usage()
		os.Exit(1)
	}

	for _, arg := range flag.Args() {
		err := filepath.Walk(arg, func(path string, info os.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				fmt.Println("Watching", path)
				err = watcher.Watch(path)
			}
			return err
		})
		if err != nil {
			panic(err)
		}
	}

	var runCmd *exec.Cmd
	runCmdInterrupted := false

	delayTimer := make(chan bool, 1)
	delayTimer <- true

	for {
		select {
		case err := <-watcher.Error:
			panic(err)

		case ev := <-watcher.Event:
			switch {
			case ev.IsCreate():
				fmt.Println("created", ev.Name)
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if err := watcher.Watch(ev.Name); err == nil {
						fmt.Println("Watching", ev.Name)
					}
				}

			case ev.IsDelete():
				fmt.Println("delete", ev.Name)

			case ev.IsModify():
				fmt.Println("modified", ev.Name)

			case ev.IsRename():
				fmt.Println("rename", ev.Name)
			}

			// (re)start 1 second delay timer
			delayTimer = make(chan bool)
			go func(c chan bool) {
				time.Sleep(*runDelayFlag)
				c <- true
			}(delayTimer)

			// send "nice" kill signal first:
			if runCmd != nil && !runCmdInterrupted {
				runCmd.Process.Signal(os.Interrupt)
				runCmdInterrupted = true
			}

		case <-delayTimer:
			if runCmd != nil {
				fmt.Println("killing old process")
				runCmd.Process.Kill()
				runCmd = nil
				runCmdInterrupted = false
			}

			if *buildCmdFlag != "" {
				fmt.Println("building...")
				cmd := exec.Command(*buildCmdFlag)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err = cmd.Run(); err != nil {
					fmt.Printf("build command finished with error: %v\n", err)
				} else if *runCmdFlag != "" {
					fmt.Println("running...")
					runCmd = exec.Command(*runCmdFlag)
					runCmd.Stdout = os.Stdout
					runCmd.Stderr = os.Stderr
					err := runCmd.Start()
					if err != nil {
						panic(err)
					}
				}
			}
		}
	}
}
