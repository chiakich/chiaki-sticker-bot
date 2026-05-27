package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/star-39/moe-sticker-bot/pkg/msbimport"
	tele "gopkg.in/telebot.v3"
)

func cleanUserDataAndDir(uid int64) bool {
	log.WithField("uid", uid).Debugln("Purging userdata...")
	users.mu.Lock()
	ud, exist := users.data[uid]
	if exist {
		workDir := ud.workDir
		delete(users.data, uid)
		activeSessionsWg.Done()
		users.mu.Unlock()
		os.RemoveAll(workDir)
		log.WithField("uid", uid).Debugln("Userdata purged from map and disk.")
		return true
	}
	users.mu.Unlock()
	log.WithField("uid", uid).Debugln("Userdata does not exist, do nothing.")
	return false
}

func cleanUserData(uid int64) bool {
	log.WithField("uid", uid).Debugln("Purging userdata...")
	users.mu.Lock()
	_, exist := users.data[uid]
	if exist {
		delete(users.data, uid)
		activeSessionsWg.Done()
		users.mu.Unlock()
		log.WithField("uid", uid).Debugln("Userdata purged from map.")
		return true
	}
	users.mu.Unlock()
	log.WithField("uid", uid).Debugln("Userdata does not exist, do nothing.")
	return false
}

func initUserData(c tele.Context, command string, state string) *UserData {
	uid := c.Sender().ID
	users.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	sID := secHex(6)
	ud := &UserData{
		state:     state,
		sessionID: sID,
		// userDir:       filepath.Join(dataDir, strconv.FormatInt(uid, 10)),
		workDir:     filepath.Join(dataDir, sID),
		command:     command,
		lineData:    &msbimport.LineData{},
		stickerData: &StickerData{},
		// stickerManage: &StickerManage{},
		ctx:    ctx,
		cancel: cancel,
	}
	users.data[uid] = ud
	activeSessionsWg.Add(1)
	users.mu.Unlock()
	// Do not anitize user work directory.
	// os.RemoveAll(ud.userDir)
	os.MkdirAll(ud.workDir, 0755)
	return ud
}

// udFromCtx safely retrieves a user's data pointer from the map.
// The lock is released before returning; callers must not call this
// if they need the map entry to remain stable across a delete.
func udFromCtx(c tele.Context) *UserData {
	users.mu.Lock()
	ud := users.data[c.Sender().ID]
	users.mu.Unlock()
	return ud
}

func getState(c tele.Context) (string, string) {
	users.mu.Lock()
	ud, exist := users.data[c.Sender().ID]
	users.mu.Unlock()
	if exist {
		return ud.command, ud.state
	}
	return "", ""
}

func checkState(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		//If bot is summoned from group chat, check command.
		if c.Chat().Type == tele.ChatGroup || c.Chat().Type == tele.ChatSuperGroup {
			log.Debugf("User %d attempted command from group chat.", c.Sender().ID)
			//For group chat, support /search only.
			if strings.HasPrefix(c.Text(), "/search@"+botName) {
				return next(c)
			} else if strings.Contains(c.Text(), "@"+botName) {
				//has metion
				return sendUnsupportedCommandForGroup(c)
			} else {
				//do nothing
				return nil
			}
		}

		command, _ := getState(c)
		if command == "" {
			log.Debugf("User %d entering command with message: %s", c.Sender().ID, c.Message().Text)
			return next(c)
		} else {
			log.Debugf("User %d already in command: %v", c.Sender().ID, command)
			return sendInStateWarning(c)
		}
	}
}

func setState(c tele.Context, state string) {
	if c == nil {
		return
	}
	users.mu.Lock()
	ud, ok := users.data[c.Sender().ID]
	users.mu.Unlock()
	if !ok {
		return
	}
	ud.state = state
}

// func setCommand(c tele.Context, command string) {
// 	uid := c.Sender().ID
// 	users.data[uid].command = command
// }
