package handlers

// fanart_thumb.go - 팬 갤러리 썸네일 보강
//
// 문제: 썸네일은 업로드할 때 이미지 서비스(127.0.0.1:8091)가 만들어 주는데, 그 서비스가
// 꺼져 있으면 썸네일 없이 저장돼서 갤러리가 원본(수백 KB~수 MB PNG)을 그대로 받아
// 느리거나 아예 안 뜨는 문제가 있었다(2026-10-01).
//
// 해결:
//   1. 이미지 서비스가 실패하면 Go 표준 라이브러리로 직접 JPEG 썸네일(긴 변 480px)을 만든다.
//   2. 갤러리 목록을 내려줄 때 DB에 썸네일 주소가 비어 있으면, 디스크에 이미 있는
//      썸네일(<이름>_thumb.webp / _thumb.jpg)을 찾아 채운다. 없으면 백그라운드로 만들어서
//      다음 요청부터 쓰게 한다(요청 자체는 기다리게 하지 않음).

import (
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const fanartThumbMax = 480

func (a *App) fanartPaths(imageURL string) (dir, name, ext string, ok bool) {
	if !strings.HasPrefix(imageURL, "/static/images/") {
		return "", "", "", false
	}
	filename := imageURL[strings.LastIndex(imageURL, "/")+1:]
	if strings.ContainsAny(filename, `/\`) || strings.Contains(filename, "..") {
		return "", "", "", false
	}
	name, ext = splitExt(filename)
	if strings.HasSuffix(name, "_thumb") {
		return "", "", "", false
	}
	return filepath.Join(a.Cfg.StaticDir, "images"), name, ext, true
}

// fanartThumbOnDisk 는 이미 만들어진 썸네일이 있으면 그 주소를 돌려준다.
func (a *App) fanartThumbOnDisk(imageURL string) string {
	dir, name, _, ok := a.fanartPaths(imageURL)
	if !ok {
		return ""
	}
	for _, suffix := range []string{"_thumb.webp", "_thumb.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, name+suffix)); err == nil {
			return "/static/images/" + name + suffix
		}
	}
	return ""
}

// makeFanartThumbGo 는 이미지 서비스 없이 Go로 JPEG 썸네일을 만든다(GIF는 움직임이 사라지므로 제외).
func (a *App) makeFanartThumbGo(imageURL string) string {
	dir, name, ext, ok := a.fanartPaths(imageURL)
	if !ok || strings.EqualFold(ext, "gif") {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, name+"."+ext))
	if err != nil {
		return ""
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		log.Printf("[팬아트 썸네일] %s 읽기 실패: %v", name, err)
		return ""
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}
	if w > fanartThumbMax || h > fanartThumbMax {
		if w >= h {
			h = h * fanartThumbMax / w
			w = fanartThumbMax
		} else {
			w = w * fanartThumbMax / h
			h = fanartThumbMax
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	// 투명 배경은 JPEG에서 검게 나오니 갤러리 카드 배경과 비슷한 어두운 회색으로 깐다.
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{24, 24, 24, 255}}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	out := filepath.Join(dir, name+"_thumb.jpg")
	tmp := out + ".tmp"
	of, err := os.Create(tmp)
	if err != nil {
		return ""
	}
	err = jpeg.Encode(of, dst, &jpeg.Options{Quality: 78})
	of.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return "/static/images/" + name + "_thumb.jpg"
}

var fanartThumbBusy sync.Map

// fillFanartThumbs 는 목록에서 썸네일이 비어 있는 항목을 디스크의 썸네일로 채우고,
// 없으면 백그라운드로 만들어 둔다.
func (a *App) fillFanartThumbs(list []map[string]any) {
	for _, m := range list {
		if s, _ := m["thumbnail_url"].(string); s != "" {
			continue
		}
		img, _ := m["image_url"].(string)
		if img == "" {
			continue
		}
		if t := a.fanartThumbOnDisk(img); t != "" {
			m["thumbnail_url"] = t
			continue
		}
		if _, busy := fanartThumbBusy.LoadOrStore(img, true); busy {
			continue
		}
		go func(img string) {
			defer fanartThumbBusy.Delete(img)
			if t := a.makeFanartThumbGo(img); t != "" {
				log.Printf("[팬아트 썸네일] 새로 만듦: %s", t)
				a.Cache.Delete("api_fanart_latest")
			}
		}(img)
	}
}
