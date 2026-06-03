package msbimport

import (
	"context"
	"strings"

	"github.com/panjf2000/ants/v2"
	log "github.com/sirupsen/logrus"
)

// Workers pool for converting webm
var wpConvertWebm, _ = ants.NewPoolWithFunc(1, wConvertWebm)

// Accepts *LineFile
func wConvertWebm(i interface{}) {
	lf := i.(*LineFile)
	defer lf.Wg.Done()
	log.Debugln("Converting in pool for:", lf)

	var err error
	ctx := lf.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		lf.CError = ctx.Err()
		return
	default:
	}
	//FFMpeg doest not support animated webp.
	//IM convert it to apng then feed to webm.
	if strings.HasSuffix(lf.OriginalFile, ".webp") {
		lf.OriginalFile, err = IMToApng(lf.OriginalFile)
		if err != nil {
			if ctx.Err() != nil {
				lf.CError = ctx.Err()
			} else {
				lf.CError = err
			}
			return
		}
	}

	lf.ConvertedFile, err = FFToWebmTGVideoContext(ctx, lf.OriginalFile, lf.ConvertToEmoji)
	if err != nil {
		lf.CError = err
	}
	log.Debugln("convert OK: ", lf.ConvertedFile)
}
