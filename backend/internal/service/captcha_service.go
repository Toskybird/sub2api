package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/redis/go-redis/v9"
)

var (
	ErrCaptchaInvalid = infraerrors.BadRequest("CAPTCHA_INVALID", "验证码错误或已过期")
	ErrCaptchaRequired = infraerrors.BadRequest("CAPTCHA_REQUIRED", "请输入验证码")
)

const (
	captchaIDLength  = 32
	captchaCodeLen   = 5
	captchaTTL       = 5 * time.Minute
	captchaRedisKey  = "captcha:"
	captchaImgWidth  = 140
	captchaImgHeight = 50
)

// CaptchaService 图形验证码服务（纯本地实现，无需外部服务）
type CaptchaService struct {
	redisClient    *redis.Client
	settingService *SettingService
}

// NewCaptchaService 创建验证码服务
func NewCaptchaService(redisClient *redis.Client, settingService *SettingService) *CaptchaService {
	return &CaptchaService{
		redisClient:    redisClient,
		settingService: settingService,
	}
}

// IsEnabled 检查是否启用图形验证码
func (s *CaptchaService) IsEnabled(ctx context.Context) bool {
	return s.settingService.IsCaptchaEnabled(ctx)
}

// CaptchaResult 生成验证码的结果
type CaptchaResult struct {
	ID    string `json:"captcha_id"`
	Image string `json:"image"` // data:image/png;base64,...
}

// Generate 生成一个新的图形验证码，返回 id 和 base64 图片
func (s *CaptchaService) Generate(ctx context.Context) (*CaptchaResult, error) {
	// 生成随机验证码字符串和 ID（复用 package 内已有的 randomHex）
	code := randomCode(captchaCodeLen)
	id := randomHex(captchaIDLength)

	// 将答案存入 Redis（大写统一，不区分大小写）
	err := s.redisClient.Set(ctx, captchaRedisKey+id, strings.ToUpper(code), captchaTTL).Err()
	if err != nil {
		return nil, err
	}

	// 生成验证码图片
	imgB64, err := renderCaptchaImage(code)
	if err != nil {
		return nil, err
	}

	return &CaptchaResult{
		ID:    id,
		Image: "data:image/png;base64," + imgB64,
	}, nil
}

// Verify 校验验证码，成功后立即删除（防重放）
func (s *CaptchaService) Verify(ctx context.Context, id, code string) error {
	if id == "" || code == "" {
		return ErrCaptchaRequired
	}

	key := captchaRedisKey + id
	stored, err := s.redisClient.GetDel(ctx, key).Result()
	if err == redis.Nil {
		return ErrCaptchaInvalid
	}
	if err != nil {
		return ErrCaptchaInvalid
	}

	if strings.ToUpper(strings.TrimSpace(code)) != stored {
		return ErrCaptchaInvalid
	}
	return nil
}

// ======================== 图片生成（纯标准库） ========================

// 字符集：去掉容易混淆的 0/O/1/I/l
const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz"

func randomCode(n int) string {
	result := make([]byte, n)
	for i := range result {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[idx.Int64()]
	}
	return string(result)
}

// renderCaptchaImage 生成验证码 PNG 图片并返回 base64
func renderCaptchaImage(code string) (string, error) {
	img := image.NewRGBA(image.Rect(0, 0, captchaImgWidth, captchaImgHeight))

	// 背景填充（浅灰）
	bgColor := color.RGBA{R: 240, G: 240, B: 240, A: 255}
	fillRect(img, 0, 0, captchaImgWidth, captchaImgHeight, bgColor)

	// 干扰线
	drawNoiseLines(img, 6)

	// 绘制字符
	charWidth := captchaImgWidth / (len(code) + 1)
	for i, ch := range code {
		x := charWidth/2 + i*charWidth
		y := captchaImgHeight/2 - 7

		// 每个字符随机颜色（深色调）
		r, _ := rand.Int(rand.Reader, big.NewInt(150))
		g, _ := rand.Int(rand.Reader, big.NewInt(150))
		b, _ := rand.Int(rand.Reader, big.NewInt(150))
		c := color.RGBA{R: uint8(r.Int64()), G: uint8(g.Int64()), B: uint8(b.Int64()), A: 255}

		drawChar(img, x, y, rune(ch), c)
	}

	// 干扰点
	drawNoiseDots(img, 80)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func drawNoiseLines(img *image.RGBA, n int) {
	w := captchaImgWidth
	h := captchaImgHeight
	for i := 0; i < n; i++ {
		x0, _ := rand.Int(rand.Reader, big.NewInt(int64(w)))
		y0, _ := rand.Int(rand.Reader, big.NewInt(int64(h)))
		x1, _ := rand.Int(rand.Reader, big.NewInt(int64(w)))
		y1, _ := rand.Int(rand.Reader, big.NewInt(int64(h)))
		r, _ := rand.Int(rand.Reader, big.NewInt(200))
		g, _ := rand.Int(rand.Reader, big.NewInt(200))
		b, _ := rand.Int(rand.Reader, big.NewInt(200))
		c := color.RGBA{R: uint8(r.Int64()), G: uint8(g.Int64()), B: uint8(b.Int64()), A: 255}
		drawLine(img, int(x0.Int64()), int(y0.Int64()), int(x1.Int64()), int(y1.Int64()), c)
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		img.SetRGBA(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func drawNoiseDots(img *image.RGBA, n int) {
	w := captchaImgWidth
	h := captchaImgHeight
	for i := 0; i < n; i++ {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(w)))
		y, _ := rand.Int(rand.Reader, big.NewInt(int64(h)))
		r, _ := rand.Int(rand.Reader, big.NewInt(256))
		g, _ := rand.Int(rand.Reader, big.NewInt(256))
		b, _ := rand.Int(rand.Reader, big.NewInt(256))
		img.SetRGBA(int(x.Int64()), int(y.Int64()), color.RGBA{
			R: uint8(r.Int64()), G: uint8(g.Int64()), B: uint8(b.Int64()), A: 255,
		})
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// 内嵌 7×12 位图字体（仅包含验证码所需字符集）
// 每个字符用 7 字节表示，每字节对应一行，bit7..bit1 对应列 0..6
var font7x12 = map[rune][12]uint8{
	'2': {0x3C, 0x66, 0x06, 0x0C, 0x18, 0x30, 0x7E, 0x00},
	'3': {0x3C, 0x66, 0x06, 0x1C, 0x06, 0x66, 0x3C, 0x00},
	'4': {0x0C, 0x1C, 0x2C, 0x4C, 0x7E, 0x0C, 0x0C, 0x00},
	'5': {0x7E, 0x60, 0x7C, 0x06, 0x06, 0x66, 0x3C, 0x00},
	'6': {0x1C, 0x30, 0x60, 0x7C, 0x66, 0x66, 0x3C, 0x00},
	'7': {0x7E, 0x06, 0x0C, 0x18, 0x18, 0x18, 0x18, 0x00},
	'8': {0x3C, 0x66, 0x66, 0x3C, 0x66, 0x66, 0x3C, 0x00},
	'9': {0x3C, 0x66, 0x66, 0x3E, 0x06, 0x0C, 0x38, 0x00},
	'A': {0x18, 0x3C, 0x66, 0x7E, 0x66, 0x66, 0x66, 0x00},
	'B': {0x7C, 0x66, 0x66, 0x7C, 0x66, 0x66, 0x7C, 0x00},
	'C': {0x3C, 0x66, 0x60, 0x60, 0x60, 0x66, 0x3C, 0x00},
	'D': {0x78, 0x6C, 0x66, 0x66, 0x66, 0x6C, 0x78, 0x00},
	'E': {0x7E, 0x60, 0x60, 0x78, 0x60, 0x60, 0x7E, 0x00},
	'F': {0x7E, 0x60, 0x60, 0x78, 0x60, 0x60, 0x60, 0x00},
	'G': {0x3C, 0x66, 0x60, 0x6E, 0x66, 0x66, 0x3C, 0x00},
	'H': {0x66, 0x66, 0x66, 0x7E, 0x66, 0x66, 0x66, 0x00},
	'J': {0x06, 0x06, 0x06, 0x06, 0x66, 0x66, 0x3C, 0x00},
	'K': {0x66, 0x6C, 0x78, 0x70, 0x78, 0x6C, 0x66, 0x00},
	'L': {0x60, 0x60, 0x60, 0x60, 0x60, 0x60, 0x7E, 0x00},
	'M': {0x63, 0x77, 0x7F, 0x6B, 0x63, 0x63, 0x63, 0x00},
	'N': {0x66, 0x76, 0x7E, 0x7E, 0x6E, 0x66, 0x66, 0x00},
	'P': {0x7C, 0x66, 0x66, 0x7C, 0x60, 0x60, 0x60, 0x00},
	'Q': {0x3C, 0x66, 0x66, 0x66, 0x6E, 0x3C, 0x06, 0x00},
	'R': {0x7C, 0x66, 0x66, 0x7C, 0x6C, 0x66, 0x66, 0x00},
	'S': {0x3C, 0x66, 0x60, 0x3C, 0x06, 0x66, 0x3C, 0x00},
	'T': {0x7E, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x00},
	'U': {0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x3C, 0x00},
	'V': {0x66, 0x66, 0x66, 0x66, 0x3C, 0x3C, 0x18, 0x00},
	'W': {0x63, 0x63, 0x63, 0x6B, 0x7F, 0x77, 0x63, 0x00},
	'X': {0x66, 0x66, 0x3C, 0x18, 0x3C, 0x66, 0x66, 0x00},
	'Y': {0x66, 0x66, 0x3C, 0x18, 0x18, 0x18, 0x18, 0x00},
	'Z': {0x7E, 0x06, 0x0C, 0x18, 0x30, 0x60, 0x7E, 0x00},
	'a': {0x00, 0x3C, 0x06, 0x3E, 0x66, 0x66, 0x3E, 0x00},
	'b': {0x60, 0x60, 0x7C, 0x66, 0x66, 0x66, 0x7C, 0x00},
	'c': {0x00, 0x3C, 0x66, 0x60, 0x60, 0x66, 0x3C, 0x00},
	'd': {0x06, 0x06, 0x3E, 0x66, 0x66, 0x66, 0x3E, 0x00},
	'e': {0x00, 0x3C, 0x66, 0x7E, 0x60, 0x66, 0x3C, 0x00},
	'f': {0x1C, 0x30, 0x30, 0x7C, 0x30, 0x30, 0x30, 0x00},
	'g': {0x00, 0x3E, 0x66, 0x66, 0x3E, 0x06, 0x3C, 0x00},
	'h': {0x60, 0x60, 0x7C, 0x66, 0x66, 0x66, 0x66, 0x00},
	'j': {0x0C, 0x00, 0x0C, 0x0C, 0x0C, 0x6C, 0x38, 0x00},
	'k': {0x60, 0x66, 0x6C, 0x78, 0x6C, 0x66, 0x66, 0x00},
	'm': {0x00, 0x66, 0x7F, 0x7F, 0x6B, 0x63, 0x63, 0x00},
	'n': {0x00, 0x7C, 0x66, 0x66, 0x66, 0x66, 0x66, 0x00},
	'p': {0x00, 0x7C, 0x66, 0x66, 0x7C, 0x60, 0x60, 0x00},
	'q': {0x00, 0x3E, 0x66, 0x66, 0x3E, 0x06, 0x06, 0x00},
	'r': {0x00, 0x6C, 0x76, 0x60, 0x60, 0x60, 0x60, 0x00},
	's': {0x00, 0x3C, 0x66, 0x38, 0x0C, 0x66, 0x3C, 0x00},
	't': {0x30, 0x30, 0x7C, 0x30, 0x30, 0x36, 0x1C, 0x00},
	'u': {0x00, 0x66, 0x66, 0x66, 0x66, 0x66, 0x3E, 0x00},
	'v': {0x00, 0x66, 0x66, 0x66, 0x3C, 0x3C, 0x18, 0x00},
	'w': {0x00, 0x63, 0x63, 0x6B, 0x7F, 0x77, 0x63, 0x00},
	'x': {0x00, 0x66, 0x3C, 0x18, 0x18, 0x3C, 0x66, 0x00},
	'y': {0x00, 0x66, 0x66, 0x3E, 0x06, 0x66, 0x3C, 0x00},
	'z': {0x00, 0x7E, 0x0C, 0x18, 0x30, 0x60, 0x7E, 0x00},
}

// drawChar 在图像的 (cx, cy) 处绘制字符（7×8 点阵，放大 2 倍）
func drawChar(img *image.RGBA, cx, cy int, ch rune, c color.RGBA) {
	upperCh := rune(strings.ToUpper(string(ch))[0])
	bitmap, ok := font7x12[upperCh]
	if !ok {
		bitmap, ok = font7x12[ch]
		if !ok {
			return
		}
	}

	scale := 2
	for row := 0; row < 8; row++ {
		b := bitmap[row]
		for col := 0; col < 7; col++ {
			if b&(0x80>>uint(col)) != 0 {
				px := cx + col*scale - 7
				py := cy + row*scale
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.SetRGBA(px+dx, py+dy, c)
					}
				}
			}
		}
	}
}
