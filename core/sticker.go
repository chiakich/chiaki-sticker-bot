package core

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/star-39/moe-sticker-bot/pkg/msbimport"
	tele "gopkg.in/telebot.v3"
)

//TODO: Shrink oversized function.

// Final stage of automated sticker submission.
// Automated means all emojis are same.
func submitStickerSetAuto(createSet bool, c tele.Context) error {
	uid := c.Sender().ID
	ud := udFromCtx(c)
	pText, teleMsg, _ := sendProcessStarted(ud, c, "Waiting...")
	ud.wg.Wait()

	defer cleanUserData(uid)

	if len(ud.stickerData.stickers) == 0 {
		log.Error("No sticker to commit!")
		return errors.New("no sticker available")
	}

	log.Debugln("stickerData summary:")
	log.Debugln(ud.stickerData)
	committedStickers := 0
	errorCount := 0
	flCount := &ud.stickerData.flCount
	ssName := ud.stickerData.id
	ssTitle := ud.stickerData.title
	ssType := ud.stickerData.stickerSetType

	//Set emojis and keywords in batch.
	for _, s := range ud.stickerData.stickers {
		s.emojis = ud.stickerData.emojis
		s.keywords = MSB_DEFAULT_STICKER_KEYWORDS
	}

	// Surface conversion progress. The sf.wg.Wait() barriers inside the commit
	// path below would otherwise stay silent for minutes while animated stickers
	// transcode, making the bot look stuck on "Preparing stickers".
	convTotal := len(ud.stickerData.stickers)
	lastConvProgressEdit := time.Time{}
	for i, sf := range ud.stickerData.stickers {
		lastConvProgressEdit = waitStickerConversionProgress(sf, i, convTotal, lastConvProgressEdit, pText, teleMsg, c)
	}

	//Try batch create.
	var batchCreateSuccess bool
	if createSet {
		err := createStickerSetBatch(ud.stickerData.stickers, c, ssName, ssTitle, ssType)
		if err != nil {
			log.Warnln("sticker.go: Error batch create:", err.Error())
		} else {
			log.Debugln("sticker.go: Batch create success.")
			batchCreateSuccess = true
			if len(ud.stickerData.stickers) < 51 {
				committedStickers = len(ud.stickerData.stickers)
			} else {
				committedStickers = 50
			}
		}
	}

	//One by one commit.
	for index, sf := range ud.stickerData.stickers {
		var err error

		//Sticker set already finished.
		if batchCreateSuccess && len(ud.stickerData.stickers) < 51 {
			go editProgressMsg(len(ud.stickerData.stickers), len(ud.stickerData.stickers), "", pText, teleMsg, c)
			break
		}
		//Sticker set is larger than 50 and batch succeeded.
		//Skip first 50 stickers.
		if batchCreateSuccess && len(ud.stickerData.stickers) > 50 {
			if index < 50 {
				continue
			}
		}
		//Batch creation failed, run normal creation procedure if createSet is true.
		if createSet && index == 0 {
			err = createStickerSet(false, sf, c, ssName, ssTitle, ssType)
			if err != nil {
				log.Errorln("create sticker set failed!. ", err)
				return err
			} else {
				committedStickers += 1
			}
			continue
		}

		go editProgressMsg(index, len(ud.stickerData.stickers), "", pText, teleMsg, c)

		err = commitSingleticker(index, flCount, false, sf, c, ud.stickerData, ssName, ssType)
		if err != nil {
			log.Warnln("execAutoCommit: a sticker failed to add.", err)
			sendOneStickerFailedToAdd(c, index, err)
			errorCount += 1
		} else {
			log.Debugln("one sticker commited. count: ", committedStickers)
			committedStickers += 1
		}
		// If encountered flood limit more than once, set a interval.
		if *flCount == 1 {
			sleepTime := 10 + rand.Intn(10)
			time.Sleep(time.Duration(sleepTime) * time.Second)
		} else if *flCount > 1 {
			sleepTime := 60 + rand.Intn(10)
			time.Sleep(time.Duration(sleepTime) * time.Second)
		}
	}

	// Tolerate at most 3 errors when importing sticker set.
	if ud.command == "import" && errorCount > 3 {
		return errors.New("too many errors importing")
	}

	if createSet {
		if ud.command == "import" {
			insertLineS(ud.lineData.Id, ud.lineData.Link, ud.stickerData.id, ud.stickerData.title, true)
			go insertEvent(c.Sender().ID, c.Sender().Username,
				strings.TrimSpace(c.Sender().FirstName+" "+c.Sender().LastName),
				"import_"+ud.lineData.Store, ud.lineData.Id, "success")
			// Only verify for import.
			// User generated sticker set might intentionally contain same stickers.
			if *flCount > 1 {
				verifyFloodedStickerSet(c, *flCount, errorCount, ud.lineData.Amount, ud.stickerData.id)
			}
		}
		insertUserS(c.Sender().ID, ud.stickerData.id, ud.stickerData.title, time.Now().Unix())
	}
	editProgressMsg(0, 0, "Success! /start", pText, teleMsg, c)
	sendSFromSS(c, ud.stickerData.id, teleMsg)
	return nil
}

func waitStickerConversionProgress(sf *StickerFile, index int, total int, lastEdit time.Time, pText string, teleMsg *tele.Message, c tele.Context) time.Time {
	done := make(chan struct{})
	go func() {
		sf.wg.Wait()
		close(done)
	}()

	start := time.Now()
	firstNotice := time.NewTimer(3 * time.Second)
	heartbeat := time.NewTicker(20 * time.Second)
	defer firstNotice.Stop()
	defer heartbeat.Stop()

	edit := func(doneCount int, activeIndex int, elapsed time.Duration, force bool) {
		now := time.Now()
		if !force && now.Sub(lastEdit) < 3*time.Second {
			return
		}
		prog := conversionProgressText(doneCount, total, activeIndex, elapsed)
		editProgressMsg(0, 0, prog, pText, teleMsg, c)
		lastEdit = now
	}

	for {
		select {
		case <-done:
			doneCount := index + 1
			force := doneCount == total
			if total <= 30 || shouldEditConversionMilestone(doneCount, total) {
				edit(doneCount, 0, 0, force)
			}
			return lastEdit
		case <-firstNotice.C:
			edit(index, index+1, time.Since(start), true)
		case <-heartbeat.C:
			edit(index, index+1, time.Since(start), true)
		}
	}
}

func shouldEditConversionMilestone(done int, total int) bool {
	if done == 1 || done == total {
		return true
	}
	if total < 4 {
		return false
	}
	return done%(total/4) == 0
}

func conversionProgressText(done int, total int, active int, elapsed time.Duration) string {
	prog := "<code>Converting / 轉檔中...\n       " + strconv.Itoa(done) + " of " + strconv.Itoa(total)
	if active > 0 {
		prog += "\n       working on " + strconv.Itoa(active) + " of " + strconv.Itoa(total)
	}
	if elapsed >= 10*time.Second {
		prog += "\n       still working " + formatShortDuration(elapsed)
	}
	return prog + "</code>"
}

func formatShortDuration(d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	if seconds < 60 {
		return strconv.Itoa(seconds) + "s"
	}
	return strconv.Itoa(seconds/60) + "m" + strconv.Itoa(seconds%60) + "s"
}

// Only fatal error should be returned.
func submitStickerManual(createSet bool, pos int, emojis []string, keywords []string, c tele.Context) error {
	ud := udFromCtx(c)
	var err error
	name := ud.stickerData.id
	title := ud.stickerData.title
	ssType := ud.stickerData.stickerSetType

	if len(ud.stickerData.stickers) == 0 {
		log.Error("No sticker to commit!!")
		return errors.New("no sticker available")
	}

	sf := ud.stickerData.stickers[pos]
	sf.emojis = emojis
	sf.keywords = keywords

	//Do not submit to goroutine when creating sticker set.
	if createSet && pos == 0 {
		defer close(ud.commitChans[pos])
		err = createStickerSet(false, sf, c, name, title, ssType)
		if err != nil {
			log.Errorln("create failed. ", err)
			return err
		} else {
			ud.stickerData.cAmount += 1
		}
		if ud.stickerData.lAmount == 1 {
			return finalizeSubmitStickerManual(c, createSet, ud)
		}
	} else {
		go func() {
			//wait for the previous commit to be done.
			if pos > 0 {
				<-ud.commitChans[pos-1]
			}

			err = commitSingleticker(pos, &ud.stickerData.flCount, false, sf, c, ud.stickerData, name, ssType)
			if err != nil {
				sendOneStickerFailedToAdd(c, pos, err)
				log.Warnln("execEmojiAssign: a sticker failed to add: ", err)
			} else {
				ud.stickerData.cAmount += 1
			}

			if pos+1 == ud.stickerData.lAmount {
				finalizeSubmitStickerManual(c, createSet, ud)
			}
			close(ud.commitChans[pos])
		}()
	}
	return nil
}

func finalizeSubmitStickerManual(c tele.Context, createSet bool, ud *UserData) error {
	if createSet {
		if ud.command == "import" {
			insertLineS(ud.lineData.Id, ud.lineData.Link, ud.stickerData.id, ud.stickerData.title, false)
			go insertEvent(c.Sender().ID, c.Sender().Username,
				strings.TrimSpace(c.Sender().FirstName+" "+c.Sender().LastName),
				"import_"+ud.lineData.Store, ud.lineData.Id, "success")
		}
		insertUserS(c.Sender().ID, ud.stickerData.id, ud.stickerData.title, time.Now().Unix())
	}
	sendExecEmojiAssignFinished(c)
	// c.Send("Success! /start")
	sendSFromSS(c, ud.stickerData.id, nil)
	endSession(c)
	return nil
}

// safeModeInput returns the best file to re-encode for safe mode.
// Prefer the original source so transparent APNG/PNG inputs keep their alpha
// instead of relying on ffmpeg to decode alpha from an already-encoded WebM.
// TGS still needs the converted file because ffmpeg cannot read it directly.
func safeModeInput(sf *StickerFile) string {
	if sf.oPath != "" && !strings.EqualFold(filepath.Ext(sf.oPath), ".tgs") {
		return sf.oPath
	}
	return sf.cPath
}

func validateStickerInput(sf *StickerFile, file string) error {
	if sf.cError != nil {
		return sf.cError
	}
	if sf.fileID != "" {
		return nil
	}
	if file == "" {
		return errors.New("converted sticker file path is empty")
	}
	if _, err := os.Stat(file); err != nil {
		return err
	}
	return nil
}

// Create sticker set if needed.
func createStickerSet(safeMode bool, sf *StickerFile, c tele.Context, name string, title string, ssType string) error {
	var file string
	var isCustomEmoji bool
	if ssType == tele.StickerCustomEmoji {
		isCustomEmoji = true
	}

	sf.wg.Wait()

	if safeMode {
		var err error
		file, err = msbimport.FFToWebmSafe(safeModeInput(sf), isCustomEmoji)
		if err != nil {
			return err
		}
	} else {
		file = sf.cPath
	}
	if err := validateStickerInput(sf, file); err != nil {
		return err
	}

	log.Debugln("createStickerSet: attempting, sticker file path:", sf.cPath)

	input := tele.InputSticker{
		Emojis:   sf.emojis,
		Keywords: sf.keywords,
	}
	if sf.fileID != "" {
		input.Sticker = sf.fileID
		input.Format = sf.format
	} else {
		input.Sticker = "file://" + file
		input.Format = guessInputStickerFormat(file)
	}

	var floodErr tele.FloodError
	for i := 0; i < 3; i++ {
		err := c.Bot().CreateStickerSet(c.Recipient(), []tele.InputSticker{input}, name, title, ssType)
		if err == nil {
			return nil
		}

		log.Errorf("createStickerSet error:%s for set:%s.", err, name)

		if errors.As(err, &floodErr) {
			sleepSec := floodErr.RetryAfter
			if sleepSec > 120 {
				log.Warnf("createStickerSet: RA=%ds too long, capping at 120s.", sleepSec)
				sleepSec = 120
			}
			log.Warnf("createStickerSet: flood limit, sleeping %ds (attempt %d/3).", sleepSec, i+1)
			time.Sleep(time.Duration(sleepSec) * time.Second)
			continue
		} else if strings.Contains(strings.ToLower(err.Error()), "video_long") {
			if safeMode {
				log.Error("safe mode DID NOT resolve video_long problem.")
				return err
			}
			log.Warnln("returned video_long, attempting safe mode.")
			return createStickerSet(true, sf, c, name, title, ssType)
		} else {
			return err
		}
	}
	return errors.New("createStickerSet: exceeded retry limit due to flood limit")
}

// Create sticker set with multiple StickerFile.
// API 7.2 feature, consider it experimental.
// If it failed, no retry, just return error and we try conventional way.
func createStickerSetBatch(sfs []*StickerFile, c tele.Context, name string, title string, ssType string) error {
	var inputs []tele.InputSticker
	log.Debugln("createStickerSetBatch: attempting, batch creation:", name)

	for i, sf := range sfs {
		sf.wg.Wait()
		file := sf.cPath
		if err := validateStickerInput(sf, file); err != nil {
			return err
		}
		input := tele.InputSticker{
			Emojis:   sf.emojis,
			Keywords: sf.keywords,
		}
		if sf.fileID != "" {
			input.Sticker = sf.fileID
			input.Format = sf.format
		} else {
			input.Sticker = "file://" + file
			input.Format = guessInputStickerFormat(file)
		}
		inputs = append(inputs, input)

		//Up to 50 stickers.
		if i == 49 {
			break
		}
	}

	return c.Bot().CreateStickerSet(c.Recipient(), inputs, name, title, ssType)
}

// Commit single sticker, retry happens inside this function.
// If all retries failed, return err.
//
// flCount counts the total flood limit for entire sticker set.
// pos is for logging only.
// effectiveRecipient returns the sticker set owner as recipient when admin
// is managing another user's set, so Telegram API calls use the correct user_id.
func effectiveRecipient(c tele.Context, sd *StickerData) tele.Recipient {
	if sd != nil && sd.ownerUID != 0 && sd.ownerUID != c.Sender().ID {
		return &tele.User{ID: sd.ownerUID}
	}
	return c.Recipient()
}

func commitSingleticker(pos int, flCount *int, safeMode bool, sf *StickerFile, c tele.Context, sd *StickerData, name string, ssType string) error {
	var err error
	var floodErr tele.FloodError
	var file string
	var isCustomEmoji bool
	if ssType == tele.StickerCustomEmoji {
		isCustomEmoji = true
	}
	sf.wg.Wait()

	if safeMode {
		file, err = msbimport.FFToWebmSafe(safeModeInput(sf), isCustomEmoji)
		if err != nil {
			return err
		}
	} else {
		file = sf.cPath
	}
	if err := validateStickerInput(sf, file); err != nil {
		return err
	}

	log.Debugln("commitSingleticker: attempting, sticker file path:", sf.cPath)
	// Retry loop.
	// For each sticker, retry at most 2 times, means 3 commit attempts in total.
	for i := 0; i < 3; i++ {
		input := tele.InputSticker{
			Emojis:   sf.emojis,
			Keywords: sf.keywords,
		}
		if sf.fileID != "" {
			input.Sticker = sf.fileID
			input.Format = sf.format
		} else {
			input.Sticker = "file://" + file
			input.Format = guessInputStickerFormat(file)
		}

		err = c.Bot().AddSticker(effectiveRecipient(c, sd), input, name)
		if err == nil {
			return nil
		}

		log.Errorf("commit sticker error:%s for set:%s.", err, name)
		// This flood limit error only happens to a specific user at a specific time.
		// It is "fake" most of time, since TDLib in API Server will automatically retry.
		// However, API always return 429.
		// Since API side will always do retry at TDLib level, message_id was also being kept so
		// no position shift will happen.
		// Flood limit error could be probably ignored.
		if errors.As(err, &floodErr) {
			// This reflects the retry count for entire SS.
			*flCount += 1
			log.Warnf("commitSticker: Flood limit encountered for user:%d, set:%s, count:%d, pos:%d", c.Sender().ID, name, *flCount, pos)
			log.Warnln("commitSticker: commit sticker retry after: ", floodErr.RetryAfter)
			if *flCount == 2 {
				sendFLWarning(c)
			}

			//Sleep
			if floodErr.RetryAfter > 60 {
				log.Error("RA too long! Telegram's bug? Attempt to sleep for 120 seconds.")
				time.Sleep(120 * time.Second)
			} else {
				extraRA := *flCount * 15
				log.Warnf("Sleeping for %d seconds due to FL.", floodErr.RetryAfter+extraRA)
				time.Sleep(time.Duration(floodErr.RetryAfter+extraRA) * time.Second)
			}

			log.Warnf("Woken up from RA sleep. ignoring this error. user:%d, set:%s, count:%d, pos:%d", c.Sender().ID, name, *flCount, pos)

			//According to collected logs, exceeding 2 flood counts will sometimes cause api server to stop auto retrying.
			//Hence, we do retry here, else, break retry loop.
			if *flCount > 2 {
				continue
			} else {
				break
			}

		} else if strings.Contains(strings.ToLower(err.Error()), "video_long") {
			// Redo with safe mode on.
			// This should happen only one time.
			// So if safe mode is on and this error still occurs, return err.
			if safeMode {
				log.Error("safe mode DID NOT resolve video_long problem.")
				return err
			} else {
				log.Warnln("returned video_long, attempting safe mode.")
				return commitSingleticker(pos, flCount, true, sf, c, sd, name, ssType)
			}
		} else if strings.Contains(err.Error(), "invalid sticker emojis") {
			log.Warn("commitSticker: invalid emoji, resetting to a star emoji and retrying...")
			input.Emojis = []string{"⭐️"}
		} else if strings.Contains(err.Error(), "400") {
			// return remaining 400 BAD REQUEST immediately to parent without retry.
			return err
		} else {
			// Handle unknown error here.
			// We simply retry for 2 more times with 5 sec interval.
			log.Warnln("commitSticker: retrying... cause:", err)
			time.Sleep(5 * time.Second)
		}
	}

	log.Warn("commitSticker: too many retries")
	if errors.As(err, &floodErr) {
		log.Warn("commitSticker: reached max retry for flood limit, assume success.")
		return nil
	}
	return err
}

func editStickerEmoji(newEmojis []string, fid string, ud *UserData) error {
	return b.SetStickerEmojiList(ud.lastContext.Recipient(), fid, newEmojis)
}

// Receive and process user uploaded media file and convert to Telegram compliant format.
// Accept telebot Media and Sticker only.
func appendMedia(c tele.Context) error {
	log.Debugf("appendMedia: Received file, MType:%s, FileID:%s", c.Message().Media().MediaType(), c.Message().Media().MediaFile().FileID)
	var files []string
	var sfs []*StickerFile
	var err error
	var workDir string
	var savePath string

	ud := udFromCtx(c)
	ud.wg.Add(1)
	defer ud.wg.Done()

	if ud.stickerData.cAmount+len(ud.stickerData.stickers) > 120 {
		return errors.New("sticker set already full 此貼圖包已滿")
	}

	//Incoming media is a sticker.
	if c.Message().Sticker != nil && ((c.Message().Sticker.Type == tele.StickerCustomEmoji) == ud.stickerData.isCustomEmoji) {
		var format string
		if c.Message().Sticker.Video {
			format = "video"
		} else {
			format = "static"
		}
		sfs = append(sfs, &StickerFile{
			fileID: c.Message().Sticker.FileID,
			format: format,
		})
		log.Debugf("One received sticker file OK. ID:%s", c.Message().Sticker.FileID)
		goto CONTINUE
	}

	workDir = ud.workDir
	savePath = filepath.Join(workDir, secHex(4))

	if c.Message().Media().MediaType() == "document" {
		savePath += filepath.Ext(c.Message().Document.FileName)
	} else if c.Message().Media().MediaType() == "animation" {
		savePath += filepath.Ext(c.Message().Animation.FileName)
	}

	err = c.Bot().Download(c.Message().Media().MediaFile(), savePath)
	if err != nil {
		return errors.New("error downloading media")
	}

	if guessIsArchive(savePath) {
		files = append(files, msbimport.ArchiveExtract(savePath)...)
	} else {
		files = append(files, savePath)
	}

	log.Debugln("appendMedia: Media downloaded to savepath:", savePath)
	for _, f := range files {
		var cf string
		var err error
		//If incoming media is already a sticker, use the file as is.
		if c.Message().Sticker != nil && ((c.Message().Sticker.Type == "custom_emoji") == ud.stickerData.isCustomEmoji) {
			cf = f
		} else {
			cf, err = msbimport.ConverMediaToTGStickerSmart(f, ud.stickerData.isCustomEmoji)
		}

		if err != nil {
			log.Warnln("Failed converting one user sticker", err)
			c.Send("Failed converting one user sticker:" + err.Error())
			continue
		}
		sfs = append(sfs, &StickerFile{
			oPath: f,
			cPath: cf,
		})
		log.Debugf("One received file OK. oPath:%s | cPath:%s", f, cf)
	}

CONTINUE:
	if len(sfs) == 0 {
		return errors.New("download or convert error")
	}

	ud.stickerData.stickers = append(ud.stickerData.stickers, sfs...)
	ud.stickerData.lAmount = len(ud.stickerData.stickers)
	replySFileOK(c, len(ud.stickerData.stickers))
	return nil
}

func guessIsArchive(f string) bool {
	f = strings.ToLower(f)
	archiveExts := []string{".rar", ".7z", ".zip", ".tar", ".gz", ".bz2", ".zst", ".rar5"}
	for _, ext := range archiveExts {
		if strings.HasSuffix(f, ext) {
			return true
		}
	}
	return false
}

func verifyFloodedStickerSet(c tele.Context, fc int, ec int, desiredAmount int, ssn string) {
	time.Sleep(31 * time.Second)
	ss, err := b.StickerSet(ssn)
	if err != nil {
		return
	}
	if desiredAmount < len(ss.Stickers) {
		log.Warnf("A flooded sticker set duplicated! floodCount:%d, errorCount:%d, ssn:%s, desired:%d, got:%d", fc, ec, ssn, desiredAmount, len(ss.Stickers))
		log.Warnf("Attempting dedup!")
		workdir := filepath.Join(dataDir, secHex(8))
		os.MkdirAll(workdir, 0755)
		for si, s := range ss.Stickers {
			if si > 0 {
				fp := filepath.Join(workdir, strconv.Itoa(si-1)+".webp")
				f := filepath.Join(workdir, strconv.Itoa(si)+".webp")
				c.Bot().Download(&s.File, f)

				if compCRC32(f, fp) {
					b.DeleteSticker(s.FileID)
				}
			}
		}
		os.RemoveAll(workdir)
	} else if desiredAmount > len(ss.Stickers) {
		log.Warnf("A flooded sticker set missing sticker! floodCount:%d, errorCount:%d, ssn:%s, desired:%d, got:%d", fc, ec, ssn, desiredAmount, len(ss.Stickers))
		c.Reply("Sorry, this sticker set seems corrupted, please check.\n抱歉, 這個貼圖包似乎有缺失貼圖, 請檢查一下.")
	} else {
		log.Infof("A flooded sticker set seems ok. floodCount:%d, errorCount:%d, ssn:%s, desired:%d, got:%d", fc, ec, ssn, desiredAmount, len(ss.Stickers))
	}
}
