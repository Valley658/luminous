// Package imgvalidate performs server-side, content-based validation of
// user-uploaded images and re-encodes them into a known-safe file before
// they are ever stored or served.
//
// [2026-09-26 보안 점검] 기존 업로드 코드는 클라이언트가 보낸 파일 확장자
// (filename)만으로 "이미지인지"를 판단하고, 실제 파일 내용은 별도의 외부
// Java 이미지 처리 서비스(javaimage.Client)에 위임했다. 그런데 그 외부
// 호출이 실패했을 때(=진짜 이미지가 아니라서 디코딩이 안 됐을 가능성이
// 높은 경우 포함) 호출부가 "실패 = 원본 파일 그대로 사용"으로 처리해버려서,
// 확장자만 .png로 바꾼 일반 텍스트 파일이 그대로 저장/서빙되는 문제가 있었다
// (POST /api/fanart/upload 등에서 재현 확인됨).
//
// 이 패키지는 Go 표준 라이브러리(+golang.org/x/image/webp)만으로 "파일
// 시그니처(매직 바이트) 검사 -> 전체 디코드로 손상 여부 확인 -> 새 파일로
// 재인코딩"을 수행해서, 외부 서비스의 가용성/정확성과 무관하게 항상
// 안전한 최종 검증을 보장한다. 재인코딩된 파일에는 디코더가 실제로 이해한
// 픽셀 데이터만 남고, 원본 파일 끝에 붙어있었을 수 있는 임의 바이트(폴리글랏/
// 위장 파일)는 전부 제거된다.
package imgvalidate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp" // 디코더 등록만 필요 (Go 표준 라이브러리엔 webp 인코더가 없음)
)

// ErrInvalidImage는 파일이 지원하는 이미지 형식으로 전혀 디코드되지 않을 때
// (=확장자만 이미지고 실제 내용은 아닌 경우, 또는 파일이 손상된 경우) 반환된다.
var ErrInvalidImage = errors.New("업로드된 파일이 유효한 이미지가 아닙니다")

// ErrImageTooLarge는 압축 해제 후 픽셀 수/한 변 길이가 허용 한도를 넘을 때
// 반환된다 (decompression bomb 방어).
var ErrImageTooLarge = errors.New("이미지 크기(픽셀)가 허용 한도를 초과합니다")

const (
	// MaxDimension: 한 변의 최대 픽셀 길이.
	MaxDimension = 10000
	// MaxPixels: 가로*세로 픽셀 수 총합의 최대값 (약 40메가픽셀).
	MaxPixels = 40_000_000
	// MaxGIFFrames: 애니메이션 GIF의 최대 프레임 수 (프레임 폭탄 방어).
	MaxGIFFrames = 1000
)

// Result는 검증/재인코딩 결과를 담는다.
type Result struct {
	Path   string // 새로 재인코딩되어 저장된 파일의 절대/상대 경로
	Format string // "png" | "jpeg" | "gif" (webp 입력은 png로 재인코딩됨)
	Ext    string // 파일 확장자 ("jpg"/"png"/"gif")
	Width  int
	Height int
}

// ValidateAndReencode는 srcPath의 파일이 진짜 이미지인지 매직 바이트+전체
// 디코드로 검증하고, 통과하면 dstDir 아래 baseName.<ext> 이름으로 재인코딩된
// 새 파일을 만든다. 실패 시 dstDir에는 아무 파일도 남기지 않는다.
// (원본 파일 srcPath는 이 함수가 지우지 않는다 - 호출부에서 정리할 것.)
func ValidateAndReencode(srcPath, dstDir, baseName string, jpegQuality int) (Result, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return Result{}, ErrImageTooLarge
	}
	if cfg.Width*cfg.Height > MaxPixels {
		return Result{}, ErrImageTooLarge
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return Result{}, err
	}

	switch format {
	case "gif":
		frames, err := gif.DecodeAll(f)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
		}
		if len(frames.Image) == 0 || len(frames.Image) > MaxGIFFrames {
			return Result{}, ErrImageTooLarge
		}
		outPath := filepath.Join(dstDir, baseName+".gif")
		out, err := os.Create(outPath)
		if err != nil {
			return Result{}, err
		}
		if err := gif.EncodeAll(out, frames); err != nil {
			out.Close()
			_ = os.Remove(outPath)
			return Result{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(outPath)
			return Result{}, err
		}
		return Result{Path: outPath, Format: "gif", Ext: "gif", Width: cfg.Width, Height: cfg.Height}, nil

	case "png", "jpeg", "webp":
		img, _, err := image.Decode(f)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
		}
		outFormat := format
		if outFormat == "webp" {
			// Go 표준 라이브러리/x/image 모두 webp 인코더가 없어서 png로 재인코딩한다.
			outFormat = "png"
		}
		ext := outFormat
		if outFormat == "jpeg" {
			ext = "jpg"
		}
		outPath := filepath.Join(dstDir, baseName+"."+ext)
		out, err := os.Create(outPath)
		if err != nil {
			return Result{}, err
		}
		switch outFormat {
		case "png":
			err = png.Encode(out, img)
		case "jpeg":
			err = jpeg.Encode(out, img, &jpeg.Options{Quality: jpegQuality})
		}
		if err != nil {
			out.Close()
			_ = os.Remove(outPath)
			return Result{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(outPath)
			return Result{}, err
		}
		b := img.Bounds()
		return Result{Path: outPath, Format: outFormat, Ext: ext, Width: b.Dx(), Height: b.Dy()}, nil

	default:
		return Result{}, ErrInvalidImage
	}
}

// RandomBaseName은 파일명 충돌/추측을 막기 위한 crypto-random 파일명(접두사
// 포함, 확장자 제외)을 만든다. 사용자가 보낸 원본 filename은 절대 저장
// 경로 구성에 쓰지 않는다 (path traversal, 파일명 추측 방지).
func RandomBaseName(prefix string, nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	if prefix == "" {
		return hex.EncodeToString(b)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
