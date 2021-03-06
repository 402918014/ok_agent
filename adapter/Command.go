package adapter

import (
	"bufio"
	"errors"
	"github.com/OpsKitchen/ok_agent/adapter/command"
	"github.com/OpsKitchen/ok_agent/util"
	"io"
	"os"
	"os/exec"
	"os/user"
)

type Command struct {
	Command  string
	Cwd      string
	Path     string
	User     string
	RunIf    string
	NotRunIf string
}

//***** interface method area *****//
func (item *Command) Brief() string {
	var brief string
	brief = "\nCommand: \t" + item.Command
	if item.Cwd != "" {
		brief += "\nCwd: \t\t" + item.Cwd
	}
	if item.User != "" {
		brief += "\nUser: \t\t" + item.User
	}
	if item.RunIf != "" {
		brief += "\nRun if: \t" + item.RunIf
	}
	if item.NotRunIf != "" {
		brief += "\nNot run if: \t" + item.NotRunIf
	}
	return brief
}

func (item *Command) Check() error {
	var err error
	var errMsg string
	var stat os.FileInfo

	//check command
	if item.Command == "" {
		errMsg = "Command is empty"
		util.Logger.Error(errMsg)
		return errors.New(errMsg)
	}

	//check cwd
	if item.Cwd != "" {
		stat, err = os.Stat(item.Cwd)
		if err != nil {
			errMsg = "Cwd does not exist"
			util.Logger.Error(errMsg)
			return errors.New(errMsg)
		} else if stat.IsDir() == false {
			errMsg = "Cwd is not a directory"
			util.Logger.Error(errMsg)
			return errors.New(errMsg)
		}
	}

	//check user
	if item.User != "" {
		_, err = user.Lookup(item.User)
		if err != nil {
			util.Logger.Error("User does not exist")
			return err
		}
	}
	return nil
}

func (item *Command) Parse() error {
	if item.User == "" {
		item.User = command.DefaultUser
	}
	return nil
}

func (item *Command) Process() error {
	//check if necessary to run command
	if item.RunIf != "" && item.fastRun(item.RunIf) == false {
		util.Logger.Debug("'RunIf' retunrs false, skip running command.")
		return nil
	}
	if item.NotRunIf != "" && item.fastRun(item.NotRunIf) == true {
		util.Logger.Debug("'NotRunIf' returns true, skip running command.")
		return nil
	}

	//run command
	return item.runWithMessage()
}

//***** interface method area *****//

func (item *Command) fastRun(line string) bool {
	var cmd *exec.Cmd
	var err error
	cmd = exec.Command(command.DefaultShell, item.User, "-c", line)
	item.setPath(cmd)
	err = cmd.Run()
	return err == nil
}

func (item *Command) runWithMessage() error {
	var cmd *exec.Cmd
	var err error
	var errPipe, outPipe io.ReadCloser
	var errReader, outReader *bufio.Reader
	var line string

	//prepare cmd object
	cmd = exec.Command(command.DefaultShell, item.User, "-c", item.Command)
	item.setCwd(cmd)
	item.setPath(cmd)

	outPipe, _ = cmd.StdoutPipe()
	errPipe, _ = cmd.StderrPipe()
	err = cmd.Start()
	if err != nil {
		util.Logger.Error("Can not start default shell: " + command.DefaultShell + "\n" + err.Error())
		return err
	} else {
		util.Logger.Info("Running command...")
	}

	//real-time output of std out
	outReader = bufio.NewReader(outPipe)
	for {
		line, err = outReader.ReadString(command.ReadStringDelimiter)
		if err != nil || io.EOF == err {
			break
		}
		util.Logger.Debug(line)
	}

	//real-time output of std err
	errReader = bufio.NewReader(errPipe)
	for {
		line, err = errReader.ReadString(command.ReadStringDelimiter)
		if err != nil || io.EOF == err {
			break
		}
		util.Logger.Debug(line)
	}
	err = cmd.Wait()
	if err != nil {
		util.Logger.Error("Failed to run commnad.")
		return err
	} else {
		util.Logger.Info("Succeed to run commnad.")
		return nil
	}
}

func (item *Command) setCwd(cmd *exec.Cmd) {
	if item.Cwd != "" {
		cmd.Dir = item.Cwd
	}
}

func (item *Command) setPath(cmd *exec.Cmd) {
	if item.Path != "" {
		cmd.Env = append(cmd.Env, command.EnvKeyPath+"="+item.Path)
	} else {
		cmd.Env = append(cmd.Env, command.EnvKeyPath+"="+os.Getenv(command.EnvKeyPath))
	}
}
