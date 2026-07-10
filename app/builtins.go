package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Builtin runs with the streams of the command it belongs to, so builtin
// output honors redirects and pipes, and returns an exit status.
type Builtin func(args []string, s IOStreams) int

func (cs *Cshell) exit(args []string, s IOStreams) int {
	code := cs.LastStatus
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil {
			code = n
		} else {
			fmt.Fprintln(s.Err, "exit: "+args[0]+": numeric argument required")
			code = 2
		}
	}
	// exit can run while the real terminal is raw (commands execute under
	// raw mode); restore it or the parent shell inherits a broken terminal
	if cs.Cleanup != nil {
		cs.Cleanup()
	}
	os.Exit(code)
	return 0
}

func (cs *Cshell) echo(args []string, s IOStreams) int {
	newline := true
	if len(args) > 0 && args[0] == "-n" {
		newline = false
		args = args[1:]
	}
	fmt.Fprint(s.Out, strings.Join(args, " "))
	if newline {
		fmt.Fprint(s.Out, "\n")
	}
	return 0
}

func (cs *Cshell) typeCmd(args []string, s IOStreams) int {
	status := 0
	for _, name := range args {
		if _, ok := cs.Commands[name]; ok {
			fmt.Fprintln(s.Out, name+" is a shell builtin")
			continue
		}
		path, err := cs.findExecutable(name)
		if err != nil {
			fmt.Fprintln(s.Err, name+": not found")
			status = 1
		} else {
			fmt.Fprintln(s.Out, name+" is "+path)
		}
	}
	return status
}

// export marks variables for child processes: `export NAME=value` sets and
// exports in one step, `export NAME` promotes an existing shell variable.
func (cs *Cshell) export(args []string, s IOStreams) int {
	status := 0
	for _, arg := range args {
		if name, value, ok := splitAssignment(arg); ok {
			cs.Vars[name] = value
			os.Setenv(name, value)
			continue
		}
		if !validVarName(arg) {
			fmt.Fprintln(s.Err, "export: "+arg+": not a valid identifier")
			status = 1
			continue
		}
		if v, ok := cs.getVar(arg); ok {
			os.Setenv(arg, v)
		}
	}
	return status
}

func (cs *Cshell) pwd(_ []string, s IOStreams) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(s.Err, "pwd: "+err.Error())
		return 1
	}
	fmt.Fprintln(s.Out, dir)
	return 0
}

func (cs *Cshell) cd(args []string, s IOStreams) int {
	if len(args) == 0 || args[0] == "~" {
		userHomeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(s.Err, "cd: "+err.Error())
			return 1
		}
		if err := os.Chdir(userHomeDir); err != nil {
			fmt.Fprintln(s.Err, "cd: "+err.Error())
			return 1
		}
		return 0
	}

	if err := os.Chdir(args[0]); err != nil {
		fmt.Fprintln(s.Err, "cd: "+args[0]+": No such file or directory")
		return 1
	}
	return 0
}
