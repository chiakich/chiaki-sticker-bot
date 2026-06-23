package msbimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	log "github.com/sirupsen/logrus"
)

func parseKakaoLink(link string, ld *LineData) (string, error) {
	var kakaoID string
	// var eid string
	var err error
	var warn string

	url, _ := url.Parse(link)

	switch url.Host {
	// Kakao web link.
	case "e.kakao.com":
		kakaoID = path.Base(url.Path)
	// Kakao mobile app share link.
	case "emoticon.kakao.com":
		_, kakaoID, err = fetchKakaoDetailsFromShareLink(link)
		if err != nil {
			return warn, err
		}
	// unknown host
	default:
		return warn, errors.New("unknown kakao link type")
	}

	var kakaoJson KakaoJson
	err = fetchKakaoMetadata(&kakaoJson, kakaoID)
	if err != nil {
		log.Debugln("Failed fetchKakaoMetadata:", err)
		return warn, err
	}

	log.Debugln("Parsed kakao link:", link)
	log.Debugln(kakaoJson.Hero, kakaoJson.Contents)

	for _, item := range kakaoJson.Contents.Items {
		if item.AnimatedUrl != "" {
			ld.DLinks = append(ld.DLinks, item.AnimatedUrl)
			ld.IsAnimated = true
		} else {
			ld.DLinks = append(ld.DLinks, item.ThumbnailUrl)
		}
	}
	if len(ld.DLinks) == 0 {
		log.Warnf("parseKakaoLink: no sticker image URLs found. id:%s link:%s", kakaoID, link)
		return warn, fmt.Errorf("Kakao metadata did not contain any sticker image URLs: %w", ErrNoStickerFound)
	}

	ld.Title = kakaoJson.Hero.Title
	ld.Id = kakaoID
	ld.Link = link
	ld.Amount = len(ld.DLinks)
	ld.Category = KAKAO_EMOTICON
	return warn, nil
}

func fetchKakaoMetadata(kakaoJson *KakaoJson, kakaoID string) error {
	apiUrl := "https://e.kakao.com/api/items/" + kakaoID
	page, err := httpGet(apiUrl)
	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(page), &kakaoJson)
	if err != nil {
		log.Errorln("Failed json parsing kakao link!", err)
		return err
	}

	log.Debugln("fetchKakaoMetadata: api link metadata fetched:", apiUrl)
	return nil
}

// Download and convert(if needed) stickers to work directory.
// *ld will be modified and loaded with local sticker information.
func prepareKakaoStickers(ctx context.Context, ld *LineData, workDir string, needConvert bool) error {
	// If no dLink, continue importing static ones.
	if ld.DLink != "" {
		return prepareKakaoZipStickers(ctx, ld, workDir, needConvert)
	}
	if len(ld.DLinks) == 0 {
		log.Warnf("prepareKakaoStickers: no sticker image URLs to download. id:%s link:%s", ld.Id, ld.Link)
		return fmt.Errorf("Kakao import had no sticker image URLs to download: %w", ErrNoStickerFound)
	}

	os.MkdirAll(workDir, 0755)

	//Initialize Files with wg added.
	//This is intended for async operation.
	//When user reached commitSticker state, sticker will be waited one by one.
	for range ld.DLinks {
		lf := &LineFile{}
		lf.Status = NewConversionStatus()
		lf.Wg.Add(1)
		ld.Files = append(ld.Files, lf)
	}

	//Download stickers one by one.
	go func() {
		for i, l := range ld.DLinks {
			select {
			case <-ctx.Done():
				log.Warn("prepareKakaoStickers received ctxDone!")
				// Mark remaining files as done with a cancellation error.
				for j := i; j < len(ld.Files); j++ {
					ld.Files[j].CError = ctx.Err()
					ld.Files[j].Wg.Done()
				}
				return
			default:
			}

			isAnimated := strings.HasSuffix(l, "-g")
			// Save without extension so format is auto-detected from content.
			// Kakao animated URLs ("-g" suffix) serve animated WebP, not GIF.
			f := filepath.Join(workDir, path.Base(l))
			err := httpDownloadWithReferer(l, f, "https://e.kakao.com/")
			if err != nil {
				log.Warnln("prepareKakaoStickers: download error:", err)
				// Fail fast: mark remaining files with the error.
				for j := i; j < len(ld.Files); j++ {
					ld.Files[j].CError = err
					ld.Files[j].Wg.Done()
				}
				return
			}
			cf := f
			if needConvert {
				if isAnimated {
					cf, err = KakaoAnimatedWebpToWebmContext(ctx, f, ld.Files[i].Status)
				} else {
					cf, err = IMToWebpTGStatic(f, false)
				}
				if err != nil {
					log.Warnln("prepareKakaoStickers: convert error:", err)
					// Fail fast: mark remaining files with the error.
					for j := i; j < len(ld.Files); j++ {
						ld.Files[j].CError = err
						ld.Files[j].Wg.Done()
					}
					return
				}
			}
			ld.Files[i].OriginalFile = f
			ld.Files[i].ConvertedFile = cf
			ld.Files[i].Wg.Done()

			log.Debug("Done process one kakao emoticon")
			log.Debugf("f:%s, cf:%s", f, cf)
		}
		log.Debug("Done process ALL kakao emoticons")
	}()
	return nil
}

func prepareKakaoZipStickers(ctx context.Context, ld *LineData, workDir string, needConvert bool) error {
	zipPath := filepath.Join(workDir, "kakao.zip")
	os.MkdirAll(workDir, 0755)

	log.Debugln("prepareKakaoZipStickers: downloading zip:", ld.DLink)
	err := fDownload(ctx, ld.DLink, zipPath)
	if err != nil {
		return err
	}

	kakaoFiles := kakaoZipExtract(zipPath, ld)
	if len(kakaoFiles) == 0 {
		log.Warnf("prepareKakaoZipStickers: no sticker files extracted. id:%s zip:%s", ld.Id, ld.DLink)
		return fmt.Errorf("Kakao zip did not contain any sticker files: %w", ErrNoStickerFound)
	}

	if filepath.Ext(kakaoFiles[0]) != ".png" {
		ld.IsAnimated = true
	}

	for _, wf := range kakaoFiles {
		lf := &LineFile{
			OriginalFile: wf,
			Status:       NewConversionStatus(),
		}
		if needConvert {
			lf.Wg.Add(1)
		}
		ld.Files = append(ld.Files, lf)
	}
	ld.Amount = len(kakaoFiles)

	if needConvert {
		go convertSToTGFormat(ctx, ld)
	}

	log.Debug("Done preparing kakao files:")
	log.Debugln(ld)

	return nil
}

// Extract and decrypt kakao zip.
func kakaoZipExtract(f string, ld *LineData) []string {
	var files []string
	workDir := fExtract(f)
	if workDir == "" {
		return nil
	}
	log.Debugln("scanning workdir:", workDir)
	files = LsFiles(workDir, []string{}, []string{})

	for _, f := range files {
		//PNG is not encrypted.
		if filepath.Ext(f) != ".png" {
			//This script decrypts the file in-place.
			commandRunWithTimeout("msb_kakao_decrypt.py", f)
		}
	}
	return files
}

// Return: kakao eid(code), kakao id, error
func fetchKakaoDetailsFromShareLink(link string) (string, string, error) {
	log.Debugln("fetchKakaoDetailsFromShareLink: Link is:", link)
	res, err := httpGetAndroidUA(link)
	if err != nil {
		log.Errorln("fetchKakaoDetailsFromShareLink: failed httpGetAndroidUA!", err)
		return "", "", err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(res))
	if err != nil {
		log.Errorln("fetchKakaoDetailsFromShareLink failed gq parsing line link!", err)
		return "", "", err
	}

	//This eid seemed to be fake.
	//There will be no fix soon.
	//In the future we might use other package to complete
	//kakao download.
	eid := ""
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		value, _ := s.Attr("id")
		if value == "app_scheme_link" {
			eid, _ = s.Attr("data-i")
		}
	})
	log.Debugln("kakao eid is:", eid)
	redirLink, _, err := httpGetWithRedirLink(link)
	if err != nil {
		return "", "", err
	}
	redirURL, err := url.Parse(redirLink)
	if err != nil {
		return "", "", err
	}
	kakaoID := path.Base(redirURL.Path)
	return eid, kakaoID, nil
}
