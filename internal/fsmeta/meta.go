package fsmeta

import (
	"fmt"
	"os"
	"os/user"
	"syscall"

	"golang.org/x/sys/unix"
)

type Permissions struct {
	OwnerUID  *int    `json:"owner_uid,omitempty"`
	OwnerName *string `json:"owner_name,omitempty"`
	GroupGID  *int    `json:"group_gid,omitempty"`
	GroupName *string `json:"group_name,omitempty"`
	Mode      string  `json:"mode"`
	Readable  bool    `json:"readable"`
	Writable  bool    `json:"writable"`
}

func Inspect(path string, info os.FileInfo) Permissions {
	permissions := Permissions{
		Mode:     fmt.Sprintf("%04o", info.Mode().Perm()),
		Readable: access(path, unix.R_OK),
		Writable: access(path, unix.W_OK),
	}

	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		uid := int(stat.Uid)
		gid := int(stat.Gid)
		permissions.OwnerUID = &uid
		permissions.GroupGID = &gid

		if ownerName, ok := lookupUser(uid); ok {
			permissions.OwnerName = &ownerName
		}
		if groupName, ok := lookupGroup(gid); ok {
			permissions.GroupName = &groupName
		}
	}

	return permissions
}

func access(path string, mode uint32) bool {
	return unix.Access(path, mode) == nil
}

func lookupUser(uid int) (string, bool) {
	entry, err := user.LookupId(fmt.Sprintf("%d", uid))
	if err != nil || entry.Username == "" {
		return "", false
	}
	return entry.Username, true
}

func lookupGroup(gid int) (string, bool) {
	entry, err := user.LookupGroupId(fmt.Sprintf("%d", gid))
	if err != nil || entry.Name == "" {
		return "", false
	}
	return entry.Name, true
}
