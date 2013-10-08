package main

import (
	"flag"
	"fmt"
	"github.com/howeyc/fsnotify"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

var buildCmdFlag = flag.String("build", "", "command used make the project, this will not be interrupted when a file is changed.")
var runCmdFlag = flag.String("run", "", "command used run the project, only run if build suceeds.")
var runDelayFlag = flag.Duration("delay", 1*time.Second, "duration to wait after file change, before restarting command.")
var restartDelayFlag = flag.Duration("restartdelay", 2*time.Second, "duration to wait after run cmd exists/crashes, before restarting.")

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s: [flags...] <glob pattern> [<glob pattern>...]\n", os.Args[0])
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

	cmdSplitRx := regexp.MustCompile("[^\\s]+") // todo: respect quoted arguments
	buildCmdArgs := cmdSplitRx.FindAllString(*buildCmdFlag, -1)
	runCmdArgs := cmdSplitRx.FindAllString(*runCmdFlag, -1)

	if len(flag.Args()) == 0 || (len(buildCmdArgs) == 0 && len(runCmdArgs) == 0) {
		flag.Usage()
		os.Exit(1)
	}

	dirWatches := map[string]map[string]bool{}

	for _, arg := range flag.Args() {
		absArg, err := filepath.Abs(arg)
		if err != nil {
			fmt.Printf("%s: %s\n", absArg, err)
			os.Exit(1)
		}
		matches, err := filepath.Glob(absArg)
		if err != nil {
			fmt.Printf("%s: %s\n", absArg, err)
			os.Exit(1)
		}
		if matches == nil {
			fmt.Printf("%s did not match any files\n", absArg)
		} else {
			for _, match := range matches {
				dirName := filepath.Dir(match)
				if _, exists := dirWatches[dirName]; !exists {
					dirWatches[dirName] = map[string]bool{}
				}
				dirWatches[dirName][absArg] = true
			}
		}
	}

	for dirName, globPatterns := range dirWatches {
		fmt.Printf("Watching %s:\n", dirName)
		for globPattern := range globPatterns {
			fmt.Printf("    %s\n", globPattern)
		}

		err = watcher.Watch(dirName)
		if err != nil {
			panic(err)
		}
	}

	var runCmd *exec.Cmd
	runCmdInterrupted := false

	delayTimer := make(chan bool, 1)
	delayTimer <- true

	var exitChan chan bool //:= nil //(make(chan bool, 1)

	for {
		select {
		case err := <-watcher.Error:
			panic(err)

		case ev := <-watcher.Event:
			rebuild := false

			if globPatterns, exists := dirWatches[filepath.Dir(ev.Name)]; exists {
				for globPattern := range globPatterns {
					matched, err := filepath.Match(globPattern, ev.Name)
					if err != nil {
						panic(err)
					}
					if matched {
						fmt.Printf("changed: %s\n", ev.Name)
						rebuild = true
					}
				}

			}

			if rebuild {
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
			}

		case <-exitChan:
			if runCmd != nil {
				runCmd.Process.Kill()
				runCmd = nil
			}
			delayTimer = make(chan bool)
			go func(c chan bool) {
				time.Sleep(*restartDelayFlag)
				c <- true
			}(delayTimer)

		case <-delayTimer:
			if runCmd != nil {
				fmt.Println("killing old process")
				runCmd.Process.Kill()
				runCmd = nil
				runCmdInterrupted = false
			}

			buildFailed := false
			if len(buildCmdArgs) > 0 {
				fmt.Println("building...")
				cmd := exec.Command(buildCmdArgs[0], buildCmdArgs[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err = cmd.Run(); err != nil {
					fmt.Printf("build command finished with error: %v\n", err)
					buildFailed = true
				}
			}
			if len(runCmdArgs) > 0 && !buildFailed {
				fmt.Println("running...")
				runCmd = exec.Command(runCmdArgs[0], runCmdArgs[1:]...)
				runCmd.Stdout = os.Stdout
				runCmd.Stderr = os.Stderr

				err := runCmd.Start()
				if err != nil {
					panic(err)
				}
				exitChan = make(chan bool, 1)
				go func(runCmd *exec.Cmd, exitChan chan bool) {
					runCmd.Wait()
					exitChan <- true
				}(runCmd, exitChan)
			}
		}
	}
}
