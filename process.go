package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Process defines a process running in octyne.
type Process struct {
	ServerConfigMutex sync.RWMutex
	ServerConfig
	Name         string
	CommandMutex sync.RWMutex
	Command      *exec.Cmd
	Online       atomic.Int32   // 0 for offline, 1 for online, 2 for failure
	Output       *io.PipeReader // Never change, don't need synchronisation.
	Input        *io.PipeWriter // Never change, don't need synchronisation.
	Stdin        io.WriteCloser // Synchronised by CommandMutex.
	Crashes      atomic.Int32
	Uptime       atomic.Int64
	ToDelete     atomic.Bool
}

// CreateProcess creates and runs a process.
func CreateProcess(name string, config ServerConfig, connector *Connector) *Process {
	// Create the process.
	output, input := io.Pipe()
	process := &Process{
		Name: name,
		//Online:       0,
		ServerConfig: config,
		Output:       output,
		Input:        input,
		//Crashes:      0,
		//Uptime:       0,
	}
	// Run the command.
	if config.Enabled {
		process.StartProcess(connector) // Error is handled by StartProcess: skipcq GSC-G104
	}
	connector.AddProcess(process)
	return process
}

// StartProcess starts the process.
func (process *Process) StartProcess(connector *Connector) error {
	name := process.Name
	info.Println("Starting process (" + name + ")")
	process.ServerConfigMutex.RLock()
	defer process.ServerConfigMutex.RUnlock()
	// Determine the command which should be run by Go and change the working directory.
	cmd := strings.Split(process.ServerConfig.Command, " ")
	command := exec.Command(cmd[0], cmd[1:]...)
	command.Dir = process.Directory
	// Run the command after retrieving the standard out, standard in and standard err.
	process.CommandMutex.Lock()
	defer process.CommandMutex.Unlock()
	process.Stdin, _ = command.StdinPipe()
	command.Stdout = process.Input
	command.Stderr = command.Stdout // We want the stderr and stdout to go to the same pipe.
	err := command.Start()
	// Check for errors.
	process.Online.Store(2)
	if err != nil {
		log.Println("Failed to start server " + name + "! The following error occured: " + err.Error())
	} else if _, err := os.FindProcess(command.Process.Pid); err != nil /* Windows */ ||
		// command.Process.Signal(syscall.Signal(0)) != nil /* Unix, disabled */ ||
		command.ProcessState != nil /* Universal */ {
		log.Println("Server " + name + " is not online!")
		// Get the stdout and stderr and log..
		var stdout bytes.Buffer
		stdout.ReadFrom(process.Output)
		log.Println("Output:\n" + stdout.String())
	} else {
		info.Println("Started server " + name + " with PID " + strconv.Itoa(command.Process.Pid))
		process.SendConsoleOutput("[Octyne] Started server " + name)
		process.Online.Store(1)
		process.Uptime.Store(time.Now().UnixNano())
	}
	// Update and return.
	process.Command = command
	go process.MonitorProcess(connector)
	return err
}

// StopProcess stops the process with SIGTERM.
func (process *Process) StopProcess() {
	info.Println("Stopping server " + process.Name)
	process.SendConsoleOutput("[Octyne] Stopping server " + process.Name)
	process.CommandMutex.RLock()
	defer process.CommandMutex.RUnlock()
	command := process.Command
	// SIGTERM works with: Java, Node, npm, yarn v1, yarn v2, PaperMC, Velocity, BungeeCord, Waterfall
	// SIGINT fails with yarn v1 and v2, hence is not used.
	command.Process.Signal(syscall.SIGTERM)
}

// KillProcess stops the process.
func (process *Process) KillProcess() {
	info.Println("Killing server " + process.Name)
	process.SendConsoleOutput("[Octyne] Killing server " + process.Name)
	process.CommandMutex.RLock()
	defer process.CommandMutex.RUnlock()
	command := process.Command
	command.Process.Kill()
	process.Online.Store(0)
}

// SendCommand sends an input to stdin of the process.
func (process *Process) SendCommand(command string) {
	process.CommandMutex.RLock()
	defer process.CommandMutex.RUnlock()
	fmt.Fprintln(process.Stdin, command)
}

// SendConsoleOutput sends console output to the stdout of the process.
func (process *Process) SendConsoleOutput(command string) {
	go fmt.Fprintln(process.Input, command) // skipcq: GO-E1007
}

// MonitorProcess monitors the process and automatically marks it as offline/online.
func (process *Process) MonitorProcess(connector *Connector) error {
	defer (func() {
		if e := recover(); e != nil {
			log.Println(e) // In case of nil pointer exception. skipcq GO-S0904
		}
	})()
	// Exit immediately if there is no process.
	process.CommandMutex.RLock()
	defer process.CommandMutex.RUnlock()
	if process.Command.Process == nil {
		return nil
	}
	// Wait for the command to finish execution.
	err := process.Command.Wait()
	// Mark as offline appropriately.
	if process.ToDelete.Load() {
		process.SendConsoleOutput("[Octyne] Server " + process.Name + " was marked for deletion, " +
			"stopped/crashed, and has now been removed.")
		if process, loaded := connector.Processes.LoadAndDelete(process.Name); loaded {
			<-time.After(5 * time.Second)
			process.Clients.Range(func(connection chan interface{}, _ string) bool {
				connection <- nil
				return true
			})
			process.Clients.Clear()
		}
	} else if process.Command.ProcessState.Success() ||
		process.Online.Load() == 0 /* SIGKILL (if done by Octyne) */ ||
		process.Command.ProcessState.ExitCode() == 130 /* SIGINT */ ||
		process.Command.ProcessState.ExitCode() == 143 /* SIGTERM */ {
		process.Online.Store(0)
		process.Uptime.Store(0)
		process.Crashes.Store(0)
		info.Println("Server " + process.Name + " has stopped.")
		process.SendConsoleOutput("[Octyne] Server " + process.Name + " has stopped.")
	} else {
		process.Online.Store(2)
		process.Uptime.Store(0)
		crashes := process.Crashes.Add(1)
		process.SendConsoleOutput("[Octyne] Server " + process.Name + " has crashed!")
		info.Println("Server " + process.Name + " has crashed!")
		if crashes <= 3 {
			process.SendConsoleOutput("[Octyne] Restarting server " + process.Name + " due to default behaviour.")
			go process.StartProcess(connector) // go'ing here is more convenient than CommandMutex.RUnlock
		}
	}
	return err
}
