# sonolus-notgarupa-background-generator

生成 NotGarupa 背景包：把原作 live 背景纹理（`liveBG_normal` / `liveBG_fever`）按演奏中的舞台布局合成——bgImage 部件 1920×1440 UI 单位、中心 y = −170、舞台根缩放 0.92——并烘焙 DarkCover 的反向减法变暗。舞台灯光、音箱、荧光棒是动画，由引擎用皮肤包绘制。

背景纹理从 Bestdori（`ingameskin/bgskin/*`，按 `-regions` 的服务器优先级取第一个拥有该 bundle 的服务器，例如 cn 独占的 `skin_bilicat`）下载到 `.cache/`；DarkCover 贴图是 APK 内置资源，随项目放在 `assets/apk/prefabs/bms/stage/`。

## 命令行

从 GitHub Release 下载对应平台的压缩包（linux / windows / darwin，amd64 / arm64；由 `.github/workflows/build.yml` 矩阵编译，推送 `v*` 标签时发布），解压后直接运行；也可以用 Go 安装或在本仓库中运行（仓库为私有，需要 `GOPRIVATE=github.com/WindowsSov8forUs/*` 和可访问仓库的 git 凭据）：

```sh
go install github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/cmd/sonolus-notgarupa-background-generator@latest
go run ./cmd/sonolus-notgarupa-background-generator [参数]   # 在本仓库中
```

```sh
sonolus-notgarupa-background-generator [-bg skin00] [-fever] [-dark-cover] [-px-per-unit 0] [-height 1640] [公共参数]
```

程序内置所需的 APK 资源，其余素材联网下载到 `-cache`（默认当前目录的 `.cache/`）。`-version` 输出版本号。

输出条目 `notgarupa-<bg>-normal` 与 `notgarupa-<bg>-fever`（若 bundle 含 fever 纹理）。画布宽 1766.4 UI 单位（0.92 下的整个 bgImage 部件），引擎在通关退场动画中按舞台 TRS 缩放、移动它；DarkCover（均匀灰）烘焙在整张画布上。

`-bg` 是普通背景（默认 `skin00`）；HABAHIRO 背景总是一并生成，它是 NotGarupa 的固定组成部分，不是可选项。`ingameskin/bgskin/habahiro`（`liveBG.png`、`liveBG_fever.png`，2048×1024）是原作对 `#HABAHIRO` live 强制使用的背景（MV 与练习模式除外），舞台布局与 skin00 相同，对应条目为 `notgarupa-habahiro-normal` 与 `notgarupa-habahiro-fever`。

## 作为 Go 库

模块根包 `background` 提供与命令行相同的生成功能，不写文件、不退出进程、不使用 sonolus-pack-go，便于嵌入服务器：

```go
import background "github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator"

results, err := background.Generate(ctx, background.Options{
	Source: background.Source{CacheDir: "cache/notgarupa", Region: "jp"},
}) // 每个背景 bundle 的 normal / fever 各一个 Result
```

- `Options` 的零值即命令行的默认值：`Backgrounds`（普通背景，默认 `background.DefaultBackgrounds`；`background.HabahiroBackground` 总是追加，其结果 `Result.Habahiro` 为 true）、`NoFever`、`NoDarkCover`、`PxPerUnit`、`Height`。
- `Source` 控制下载：`CacheDir`、`Regions`（服务器优先级，默认 jp、en、tw、cn、kr）、`Offline`、`HTTPClient`、`Log`；`Generate` 的 `ctx` 可取消下载。多个生成器可以共用同一个 `CacheDir`（缓存写入是原子的）。
- 返回 `[]*background.Result`：`Item`（sonolus-core-go 的 `database.DatabaseBackgroundItem`，资源字段留空）、Data（`resource.BackgroundData`）、Configuration、Image、Thumbnail（PNG）以及来源 bundle / 文件 / 变体。
- `Result.Files()` 按 item.json 的字段名返回资源字节："thumbnail"、"data"、"configuration"（均已 gzip 压缩）、"image"。
- `background.WriteSource(dir, results...)` 把结果写成 sonolus-pack 源目录（命令行就是 `Generate` + `WriteSource` + sonolus-pack-go）。

在 sonolus-server-go 中注册（作为依赖同样需要 `GOPRIVATE`）（资源字段填入仓库地址）：

```go
for _, r := range results {
	files, err := r.Files()
	if err != nil { /* … */ }
	it := r.Item
	it.Thumbnail, it.Data, it.Image, it.Configuration = s.AddBytes(files["thumbnail"], ""), s.AddBytes(files["data"], ""), s.AddBytes(files["image"], ""), s.AddBytes(files["configuration"], "")
	s.Background.Items = append(s.Background.Items, sonolusserver.BackgroundItemModel{DatabaseBackgroundItem: it})
}
```

## 公共参数（命令行）

| 参数 | 默认值 | 作用 |
|---|---|---|
| `-out` | `out/source` | 重新生成的 sonolus-pack 源目录 |
| `-pack` | `out/pack` | 用 sonolus-pack-go 打包到该目录（空字符串则跳过） |
| `-cache` | `.cache` | 下载缓存目录 |
| `-regions` | `jp,en,tw,cn,kr` | Bestdori 服务器优先级。各服有独占素材，每个 bundle 取自第一个拥有它的服务器（该 bundle 的全部文件都来自同一服）；某服没有时记录在缓存中，`-offline` 重跑时选择相同的服务器 |
| `-offline` | false | 只使用缓存中的下载 |
| `-v` | false | 输出下载日志 |
| `-version` | false | 输出版本号后退出 |

公共代码（Bestdori 下载与缓存、图像处理、打包输出等）放在本项目 `internal/` 下，项目可单独构建与运行。资源名称约定见 `sonolus-notgarupa-engine/docs/RESOURCE_CONTRACT.md`。
