package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

type gridConfig struct {
	input       string
	output      string
	rows        int
	cols        int
	cellWidth   int
	cellHeight  int
	margin      int
	jpegQuality int
	background  color.Color
}

type videoMetadata struct {
	duration float64
	width    int
	height   int
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		exitWithError(err)
	}

	if err := ensureExecutables(); err != nil {
		exitWithError(err)
	}

	meta, err := probeVideo(cfg.input)
	if err != nil {
		exitWithError(err)
	}

	if cfg.cellHeight == 0 {
		cfg.cellHeight = inferCellHeight(cfg.cellWidth, meta.width, meta.height)
	}

	totalFrames := cfg.rows * cfg.cols
	timestamps := sampleTimestamps(meta.duration, totalFrames)
	frames := make([]image.Image, totalFrames)

	for i, ts := range timestamps {
		frame, captureErr := captureFrame(cfg.input, ts)
		if captureErr != nil {
			exitWithError(fmt.Errorf("提取第 %d 张截图失败: %w", i+1, captureErr))
		}
		frames[i] = scaleToFit(frame, cfg.cellWidth, cfg.cellHeight)
	}

	collage := composeGrid(frames, cfg)

	if err := saveImage(collage, cfg.output, cfg.jpegQuality); err != nil {
		exitWithError(err)
	}

	fmt.Printf("已生成九宫格截图: %s\n", cfg.output)
}

func parseFlags() (*gridConfig, error) {
	cfg := &gridConfig{}
	var bgColor string

	flag.StringVar(&cfg.input, "input", "", "输入视频文件路径 (必填)")
	flag.StringVar(&cfg.output, "output", "preview.png", "输出图片路径，格式根据扩展名自动决定")
	flag.IntVar(&cfg.rows, "rows", 3, "九宫格行数")
	flag.IntVar(&cfg.cols, "cols", 3, "九宫格列数")
	flag.IntVar(&cfg.cellWidth, "cell-width", 320, "单个截图目标宽度 (像素)")
	flag.IntVar(&cfg.cellHeight, "cell-height", 0, "单个截图目标高度 (像素)，为 0 时按视频比例自适应")
	flag.IntVar(&cfg.margin, "margin", 8, "截图之间及四周的边距 (像素)")
	flag.IntVar(&cfg.jpegQuality, "quality", 90, "输出 JPEG 时的质量 (1-100)")
	flag.StringVar(&bgColor, "background", "#FFFFFF", "背景色 (HEX，例如 #202020 或 #FFFFFFFF)")

	flag.Parse()

	if cfg.input == "" {
		return nil, errors.New("必须指定输入视频路径 --input")
	}

	if cfg.rows <= 0 || cfg.cols <= 0 {
		return nil, errors.New("rows 和 cols 必须为正整数")
	}

	if cfg.cellWidth <= 0 {
		return nil, errors.New("cell-width 必须为正整数")
	}

	if cfg.margin < 0 {
		return nil, errors.New("margin 不能为负数")
	}

	if cfg.jpegQuality < 1 || cfg.jpegQuality > 100 {
		return nil, errors.New("quality 范围为 1-100")
	}

	colorValue, err := parseHexColor(bgColor)
	if err != nil {
		return nil, err
	}
	cfg.background = colorValue

	return cfg, nil
}

func ensureExecutables() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("未找到 ffmpeg，请先安装并确保其在 PATH 中")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return errors.New("未找到 ffprobe，请先安装并确保其在 PATH 中")
	}
	return nil
}

func probeVideo(path string) (*videoMetadata, error) {
	duration, err := probeDuration(path)
	if err != nil {
		return nil, err
	}

	width, height, err := probeResolution(path)
	if err != nil {
		return nil, err
	}

	if duration <= 0 {
		return nil, fmt.Errorf("未能获取视频时长或时长为 0")
	}
	return &videoMetadata{duration: duration, width: width, height: height}, nil
}

func probeDuration(path string) (float64, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("获取视频时长失败: %w", err)
	}

	value, parseErr := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if parseErr != nil {
		return 0, fmt.Errorf("解析视频时长失败: %w", parseErr)
	}
	return value, nil
}

func probeResolution(path string) (int, int, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("获取视频分辨率失败: %w", err)
	}

	tokens := strings.Fields(strings.TrimSpace(string(output)))
	if len(tokens) == 0 {
		return 0, 0, fmt.Errorf("解析视频分辨率失败: 输出为空")
	}

	parts := strings.Split(tokens[0], "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("解析视频分辨率失败: %s", strings.TrimSpace(string(output)))
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("解析宽度失败: %w", err)
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("解析高度失败: %w", err)
	}

	return width, height, nil
}

func sampleTimestamps(duration float64, count int) []float64 {
	if count <= 0 {
		return nil
	}
	if count == 1 {
		return []float64{duration / 2}
	}

	timestamps := make([]float64, count)
	interval := duration / float64(count+1)
	for i := 0; i < count; i++ {
		timestamps[i] = interval * float64(i+1)
	}
	return timestamps
}

func captureFrame(videoPath string, timestamp float64) (image.Image, error) {
	ts := fmt.Sprintf("%.3f", timestamp)
	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "error",
		"-ss", ts,
		"-i", videoPath,
		"-frames:v", "1",
		"-f", "image2pipe",
		"-vcodec", "png",
		"-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	img, err := png.Decode(stdout)
	if err != nil {
		_ = cmd.Wait()
		return nil, err
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return img, nil
}

func scaleToFit(img image.Image, maxWidth, maxHeight int) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	scale := math.Min(float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	if scale <= 0 || math.IsInf(scale, 0) || math.IsNaN(scale) {
		scale = 1
	}

	newWidth := int(math.Round(float64(width) * scale))
	newHeight := int(math.Round(float64(height) * scale))

	if newWidth <= 0 {
		newWidth = 1
	}
	if newHeight <= 0 {
		newHeight = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

func composeGrid(frames []image.Image, cfg *gridConfig) image.Image {
	totalWidth := cfg.cols*cfg.cellWidth + (cfg.cols+1)*cfg.margin
	totalHeight := cfg.rows*cfg.cellHeight + (cfg.rows+1)*cfg.margin

	canvas := image.NewRGBA(image.Rect(0, 0, totalWidth, totalHeight))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: cfg.background}, image.Point{}, draw.Src)

	for idx, frame := range frames {
		if frame == nil {
			continue
		}
		row := idx / cfg.cols
		col := idx % cfg.cols

		cellX := cfg.margin + col*(cfg.cellWidth+cfg.margin)
		cellY := cfg.margin + row*(cfg.cellHeight+cfg.margin)

		frameBounds := frame.Bounds()
		offsetX := cellX + (cfg.cellWidth-frameBounds.Dx())/2
		offsetY := cellY + (cfg.cellHeight-frameBounds.Dy())/2

		draw.Draw(canvas, image.Rect(offsetX, offsetY, offsetX+frameBounds.Dx(), offsetY+frameBounds.Dy()), frame, frameBounds.Min, draw.Over)
	}

	return canvas
}

func saveImage(img image.Image, path string, quality int) error {
	if err := ensureOutputDir(path); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return jpeg.Encode(file, img, &jpeg.Options{Quality: quality})
	case ".png", "":
		return png.Encode(file, img)
	default:
		return fmt.Errorf("不支持的输出格式: %s", ext)
	}
}

func ensureOutputDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func parseHexColor(value string) (color.Color, error) {
	hex := strings.TrimPrefix(strings.TrimSpace(value), "#")
	switch len(hex) {
	case 6:
		r, err := strconv.ParseUint(hex[0:2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		g, err := strconv.ParseUint(hex[2:4], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		b, err := strconv.ParseUint(hex[4:6], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		return color.RGBA{uint8(r), uint8(g), uint8(b), 255}, nil
	case 8:
		r, err := strconv.ParseUint(hex[0:2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		g, err := strconv.ParseUint(hex[2:4], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		b, err := strconv.ParseUint(hex[4:6], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		a, err := strconv.ParseUint(hex[6:8], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析背景色失败: %w", err)
		}
		return color.RGBA{uint8(r), uint8(g), uint8(b), uint8(a)}, nil
	default:
		return nil, fmt.Errorf("背景色格式必须为 #RRGGBB 或 #RRGGBBAA: %s", value)
	}
}

func inferCellHeight(cellWidth, videoWidth, videoHeight int) int {
	if videoWidth <= 0 || videoHeight <= 0 {
		return int(float64(cellWidth) * 9.0 / 16.0)
	}
	ratio := float64(videoHeight) / float64(videoWidth)
	height := int(math.Round(float64(cellWidth) * ratio))
	if height <= 0 {
		height = int(float64(cellWidth) * 9.0 / 16.0)
	}
	return height
}

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}
