# 视频九宫格截图工具

使用 Go 语言编写的命令行工具，通过调用 `ffmpeg`/`ffprobe` 从视频中采样多张截图，并生成自定义行列的九宫格拼接图。

## 依赖

- Go 1.21+
- `ffmpeg` 与 `ffprobe`，需放入可执行路径（如使用 `brew install ffmpeg` 安装）

## 构建

```bash
go build -o video-preview-image
```

## 使用示例

```bash
./video-preview-image \
  --input sample.mp4 \
  --output previews/sample-grid.png \
  --rows 3 \
  --cols 3 \
  --cell-width 320 \
  --margin 16 \
  --background "#202020" \
  --quality 90
```

### 参数说明

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--input` | *(必填)* | 输入视频路径 |
| `--output` | `preview.png` | 输出图片路径，后缀决定图片格式（支持 `.png`, `.jpg`/`.jpeg`） |
| `--rows` | `3` | 拼接行数 |
| `--cols` | `3` | 拼接列数 |
| `--cell-width` | `320` | 单格目标宽度（像素） |
| `--cell-height` | `0` | 单格目标高度，0 表示按视频纵横比自适应 |
| `--margin` | `16` | 单格之间与边缘的间距（像素） |
| `--background` | `#000000` | 背景色（支持 `#RRGGBB` 或 `#RRGGBBAA`） |
| `--quality` | `90` | 输出 JPEG 时的质量 (1-100) |

## 工作流程

1. 使用 `ffprobe` 读取视频时长与分辨率。
2. 按行列数量均匀计算时间点，利用 `ffmpeg` 捕获对应帧。
3. 将截图缩放至单格尺寸范围内并居中摆放。
4. 输出最终拼图，支持 PNG 与 JPEG。

在遇到异常时，工具会输出错误信息并返回非零状态码。
