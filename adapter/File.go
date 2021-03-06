package adapter

import (
	"errors"
	"github.com/OpsKitchen/ok_agent/adapter/file"
	"github.com/OpsKitchen/ok_agent/util"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

type File struct {
	FilePath    string
	User        string
	Group       string
	Permission  string
	FileType    string
	FileContent string
	NoTruncate  bool
	Target      string
	//internal fields, not for json
	gid, uid  uint32
	pathExist bool
	perm      os.FileMode
}

//***** interface method area *****//
func (item *File) Brief() string {
	var brief string
	brief = "\nFile path: \t" + item.FilePath + "\nFile type: \t" + item.FileType
	if item.User != "" {
		brief += "\nUser: \t\t" + item.User
	}
	if item.Group != "" {
		brief += "\nGroup: \t\t" + item.Group
	}
	if item.Permission != "" {
		brief += "\nPermission: \t" + item.Permission
	}
	return brief
}

func (item *File) Check() error {
	var errMsg string
	//check file type
	if item.FileType == "" {
		errMsg = "File type is empty"
		util.Logger.Error(errMsg)
		return errors.New(errMsg)
	}
	if item.FileType != file.FileTypeDir && item.FileType != file.FileTypeFile && item.FileType != file.FileTypeLink {
		errMsg = "File type is invalid"
		util.Logger.Error(errMsg)
		return errors.New(errMsg)
	}

	//check file path
	if item.FilePath == "" {
		errMsg = "File path is empty"
		util.Logger.Error(errMsg)
		return errors.New(errMsg)
	}
	if item.FilePath == file.FilePathRoot {
		errMsg = "File path is root"
		util.Logger.Error(errMsg)
		return errors.New(errMsg)
	}

	//check symbol link target
	if item.FileType == file.FileTypeLink && item.Target == "" {
		errMsg = "Symbol link target is empty"
		util.Logger.Error(errMsg)
	}

	return nil
}

func (item *File) Parse() error {
	var err error
	var filePerm uint64
	var gid, uid uint64
	var groupObj *user.Group
	var userObj *user.User

	//convert string permission to os.FileMode
	if item.Permission == "" {
		switch item.FileType {
		case file.FileTypeDir:
			item.perm = os.FileMode(file.DefaultPermDir)
		case file.FileTypeFile:
			item.perm = os.FileMode(file.DefaultPermFile)
		case file.FileTypeLink:
			item.perm = os.FileMode(file.DefaultPermLink)
		}
	} else {
		filePerm, err = strconv.ParseUint(item.Permission, 8, 32)
		if err != nil {
			util.Logger.Error("File permission is invalid")
			return err
		}
		item.perm = os.FileMode(filePerm)
	}

	//convert string user/group to uint32
	if item.User != "" && item.Group != "" {
		groupObj, err = user.LookupGroup(item.Group)
		if err != nil {
			util.Logger.Error("Group does not exist")
			return err
		}

		userObj, err = user.Lookup(item.User)
		if err != nil {
			util.Logger.Error("User does not exist")
			return err
		}
		gid, _ = strconv.ParseUint(groupObj.Gid, 10, 32)
		uid, _ = strconv.ParseUint(userObj.Uid, 10, 32)
		item.gid = uint32(gid)
		item.uid = uint32(uid)
	}

	return nil
}

func (item *File) Process() error {
	var err error
	//check path exist
	err = item.checkFilePathExistence()
	if err != nil {
		return err
	}

	//create parent dir
	err = item.createParentDir()
	if err != nil {
		return err
	}

	switch item.FileType {
	case file.FileTypeDir:
		return item.processDir()
	case file.FileTypeFile:
		return item.processFile()
	case file.FileTypeLink:
		return item.processLink()
	}
	return nil
}

//***** interface method area *****//

func (item *File) processDir() error {
	var err error

	//create dir
	if item.pathExist == false {
		err = os.Mkdir(item.FilePath, item.perm)
		if err != nil {
			util.Logger.Error("Failed to create directory: " + err.Error())
			return err
		}
		util.Logger.Info("Succeed to create directory.")
	} else {
		util.Logger.Debug("Directory already exists, skip creating.")
	}

	//change permission
	err = item.changeOwnership()
	if err != nil {
		return err
	}

	//change permission
	err = item.changePermission()
	if err != nil {
		return err
	}

	return nil
}

func (item *File) processFile() error {
	var err error
	var skipWriteContent bool

	//create new file
	if item.pathExist == false {
		_, err = os.Create(item.FilePath)
		if err != nil {
			util.Logger.Error("Failed to create file: " + err.Error())
			return err
		}
		util.Logger.Info("Succeed to create file.")
		skipWriteContent = item.FileContent == ""
	} else {
		util.Logger.Debug("File already exists, skip creating.")
		if item.FileContent == "" { //content is empty, check if NoTruncate is true
			skipWriteContent = item.NoTruncate
		} //else, content not empty, ignore NoTruncate, skipWriteContent = false
	}

	//write content
	if skipWriteContent == false {
		err = item.writeContent()
		if err != nil {
			return err
		}
	}

	//change user and group
	err = item.changeOwnership()
	if err != nil {
		return err
	}

	//change permission
	err = item.changePermission()
	if err != nil {
		return err
	}

	return nil
}

func (item *File) processLink() error {
	var err error
	var linkTarget string

	//remove link if necessary
	if item.pathExist == true {
		linkTarget, _ = os.Readlink(item.FilePath)
		if linkTarget == item.Target {
			util.Logger.Debug("Symbol link with correct target already exists, skip creating.")
			return nil
		}
		err = os.Remove(item.FilePath)
		if err != nil {
			util.Logger.Error("Failed to remove symbol old symbol link: " + err.Error())
			return err
		}
	}

	//create link
	err = os.Symlink(item.Target, item.FilePath)
	if err != nil {
		util.Logger.Error("Failed to create link: " + err.Error())
		return err
	}
	util.Logger.Info("Succeed to create symbol link.")
	return nil
}

func (item *File) changeOwnership() error {
	var err error
	var convertedOk bool
	var stat os.FileInfo
	var stat_t *syscall.Stat_t

	if item.User != "" && item.Group != "" {
		stat, err = os.Stat(item.FilePath)
		if err == nil {
			stat_t, convertedOk = stat.Sys().(*syscall.Stat_t)
			if convertedOk {
				//user and group is already right, no need to change
				if item.gid == stat_t.Gid && item.uid == stat_t.Uid {
					util.Logger.Debug("File ownership is correct, skip changing ownership.")
					return nil
				}
			}
		}

		err = os.Lchown(item.FilePath, int(item.gid), int(item.gid))
		if err != nil {
			util.Logger.Error("Failed to change ownership: " + err.Error())
			return err
		}
		util.Logger.Info("Succeed to change ownership.")
	}
	return nil
}

func (item *File) changePermission() error {
	var err error
	var stat os.FileInfo

	if item.Permission != "" {
		stat, err = os.Stat(item.FilePath)
		if stat.Mode().Perm() == item.perm {
			util.Logger.Debug("File permission is correct, skip changing permission.")
			return nil
		}
		err = os.Chmod(item.FilePath, item.perm)
		if err != nil {
			util.Logger.Error("Failed to change permission: " + err.Error())
			return err
		}
		util.Logger.Info("Succeed to change file permission.")
	}
	return nil
}

func (item *File) checkFilePathExistence() error {
	var err error
	var errMsg string
	var stat os.FileInfo

	stat, err = os.Lstat(item.FilePath)
	if err != nil { //path not exist, do nothing
		return nil
	}

	switch item.FileType {
	case file.FileTypeDir:
		if stat.Mode().IsDir() == false {
			errMsg = "Path name already exists, but is not a directory"
			util.Logger.Error(errMsg)
			return errors.New(errMsg)
		}
	case file.FileTypeFile:
		if stat.Mode().IsRegular() == false {
			errMsg = "Path name already exists, but is not a regular file"
			util.Logger.Error(errMsg)
			return errors.New(errMsg)
		}
	case file.FileTypeLink:
		if stat.Mode()&os.ModeSymlink == 0 { // is not symbol link
			errMsg = "Path name already exists, but is not a symbol link"
			util.Logger.Error(errMsg)
			return errors.New(errMsg)
		}
	}
	item.pathExist = true
	return nil
}

func (item *File) createParentDir() error {
	var err error
	var errMsg string
	var parentDir string
	var stat os.FileInfo

	parentDir = filepath.Dir(item.FilePath)
	stat, err = os.Stat(parentDir)
	if err == nil { //path exist
		if stat.Mode().IsDir() == false {
			errMsg = "Parent directory name already exists, but is not a directory: " + parentDir
			util.Logger.Error(errMsg)
			return errors.New(errMsg)
		}
		util.Logger.Debug("Parent directory already exists, skip creating.")
		return nil
	}

	err = os.MkdirAll(parentDir, item.perm)
	if err != nil {
		util.Logger.Error("Failed to create parent directory: " + parentDir + "\n" + err.Error())
		return err
	}
	util.Logger.Info("Succeed to create parent directory: ", parentDir)
	return nil
}

func (item *File) writeContent() error {
	var contentBytes []byte
	var err error
	contentBytes, _ = ioutil.ReadFile(item.FilePath)
	if item.FileContent == string(contentBytes) {
		util.Logger.Debug("File content is correct, skip writing content.")
		return nil
	}
	err = ioutil.WriteFile(item.FilePath, []byte(item.FileContent), item.perm)
	if err != nil {
		util.Logger.Error("Failed to write content: " + err.Error())
		return err
	}
	util.Logger.Info("Succeed to write content.")
	return nil
}
