# sonolus-notgarupa-background-generator

生成 NotGarupa 背景包：原作主数据（JP 10.1.4）中的每一个 live 背景——活动背景、Collaboration / Limited 特殊背景与 HABAHIRO 背景——按演奏中的舞台布局合成（bgImage 部件 1920×1440 UI 单位、中心 y = −170、舞台根缩放 0.92），特殊背景的舞台特效按演奏中的状态合成，并烘焙 DarkCover 的反向减法变暗。舞台灯光、音箱、荧光棒是动画，由引擎用皮肤包绘制。

默认运行即生成全部条目，没有选择单个背景的参数。

## 条目

每个背景 bundle 生成一个条目 `garupa-<bundle>`（普通背景 `liveBG_normal`；不生成 fever 背景，见「Fever 背景」），标题为原作名称（如「デフォルト背景」「ペルソナコラボ」「HABAHIRO」），共 23 个条目。名称来自原作各服主数据（`internal/master/skins.json` 的 `names`）：每种语言用对应服的名称——日文 JP、英文 EN、繁体中文 TW、简体中文 CN，该服没有时取 JP、EN、TW、CN 中第一个有的名称（例如 Roselia 的 lane：「Roselia Lane」「Roselia的軌跡」「Roselia的按键条皮肤」）；普通设置用各行的皮肤名（`TYPE1`、`レーンスキン00`、`エフェクトスキン00`、`SEスキン00`、`デフォルト背景` 等），Collaboration 行用季节描述（`MasterSeasonSpecial.description`，如 `ペルソナコラボ`），Limited 行用限定皮肤名（如 `エイプリルフール2019`）；同一组件出现在多行时取 Limited 行，其次取季节最早的行。特殊行替换的各轴是一个固定组合，条目名只写一次（该组合的第一个 bundle），标题只写一次（该行的名称）。条目的作者为 `GirlsBandParty`，副标题为游戏名。包中（`db.json`）的条目按原作主数据的顺序排列（即 `Items()` 的顺序）：先是普通设置，按各轴 master 行的顺序组合（默认组合在最前）；其后是特殊组合，按 Collaboration、Limited 的行号；HABAHIRO 在最后。sonolus-pack 按目录名列出条目，打包后再按这个顺序重排。

| 来源 | bundle |
|---|---|
| 活动背景（主数据 MasterSkinBackground） | `skin00`、`skin02`、`skin03` |
| 特殊背景（Collaboration / Limited 的背景组件） | `skin_persona`、`skin_miku`、`skin_cafe`、`skin_maid`、`skin_gbp2020`、`skin_coin`、`skin_witch`、`skin_april2021`、`skin_stage`、`skin_delta`、`skin_5th`、`skin_bike`、`skin_satan`、`skin_april2019`、`skin_collabo23_summer_g`、`skin_collabo23_winter_d`、`skin_april_2024`、`skin_collabo24_autumn_i`、`skin_collabo25_autumn_s` |
| HABAHIRO（原作对 `#HABAHIRO` live 强制使用，MV 与练习模式除外） | `habahiro`，`Result.Habahiro` 为 true |

未生成的背景（理由同样记录在 `internal/master` 的 `Excluded` 中）：

- `skin01`（剧情活动）、`skin04`（Live Try 活动）：官方 CDN 与所有 Bestdori 服务器上都没有该 bundle。
- `skin_teamlivefestival`（团队 Live 祭典）：bundle 中没有 live 背景纹理（liveBG），只有团队 Live 祭典多人模式按队伍的舞台物件合成它；单人 live 不会显示。

## 合成

- **底图**：`liveBG_normal.png` / `liveBG.png` 取自 Bestdori，按 `-regions` 的服务器优先级取第一个拥有该 bundle 的服务器。拉伸到 bgImage 部件，画布宽 1766.4 UI 单位（0.92 下的整个部件，引擎在通关退场动画中按舞台 TRS 缩放、移动它）、纵向从部件顶边 y = 506 到 y = −820（部件之上原本只是部件够不着的纯黑，生成时校验后裁掉，由背景色 `#000000` 代替；Preview 中 Sonolus 自己铺背景，否则在比 16:9 方的屏幕上会露出这段黑边），分辨率为纹理原生密度。`fit` 为 `cover`。
- **活动背景的分层纹理**：`skin02`、`skin03` 的 `liveBG_layer1/2` 只在挑战 Live 与对邦 Live 模式中使用（原作 BgManager 的对应模式初始化）；普通 live 只显示 `liveBG_normal`，所以不合成。
- **舞台特效**：特殊背景 bundle 带有舞台特效 prefab（`stageEffect`，InGameStageBgEffector），原作把它放在舞台根上、与 bgImage 部件同一坐标系（顶边 y = 550）。程序从官方 CDN 的原始 UnityFS bundle 读取 prefab，渲染其演奏中的状态：
  - Animator 的 StartAnimation / ClearAnimation 只在开场与通关播放，所有状态都写默认值（Write Defaults），并回到空的 None 状态；它们通过事件触发的补间在同一段动画中反向播放。因此演奏中显示的是 prefab 的序列化状态。
  - 渲染内容：激活的 UITexture，透明度沿 NGUI 的 UIRect 链相乘（部件颜色 alpha、面板 alpha、AnimatedAlpha、实例化时即启用的 TweenAlpha 的起始值）；面板裁剪（软裁剪，尺寸为 0 时全部裁掉）；`Unlit/Depth Cutout` 深度遮挡；`Unlit/Transparent Colored`（alpha 混合）与 `Mobile/Particles/Additive`（加色）在线性色彩空间中混合。绘制顺序按面板 render queue、面板深度、部件深度。舞台特效的 render queue（3100–3250）位于 bgImage 与 DarkCover（3490）之间，所以 DarkCover 也盖在舞台特效上。
  - 遇到无法静态表达的可见内容（不支持的部件类型、着色器、纹理格式，屏幕平面外的旋转，实例化即启用的位移/旋转/缩放补间）时报错，不会静默丢弃。
- **DarkCover**：在线性空间中减去灰 121 × linear(0.25)，烘焙在整张画布上（含舞台特效）。

### 各特殊背景演奏中的舞台特效

| 背景 | 演奏中可见的特效 |
|---|---|
| `skin_delta`、`skin_collabo23_winter_d` | 左右音箱（`speaker_L` / `speaker_R`，228×312，位于 (±627, 82)）。地面光带与聚光灯只在通关动画中出现 |
| `skin_gbp2020` | 无：霓虹招牌被 Depth Cutout 遮罩完全遮住，霓虹点亮只在开场动画中出现 |
| `skin_persona` | 无：地面光格与镜球的面板 / 容器 alpha 为 0，只在开场与通关动画中出现 |
| `skin_miku` | 无：地面灯光所在面板的裁剪区域宽度为 0，音箱与均衡器所在的组 alpha 为 0，只在开场与通关动画中出现 |
| `skin_satan` | 无：刻印与文字特效未激活，只在开场与通关动画中出现 |
| `skin_maid` | 无静态内容；雪花是循环的粒子系统 |
| 其余特殊背景与 `habahiro` | prefab 只有空的 `stageEffect` 节点 |

静态背景无法表达的部分：

- 开场与通关的舞台特效动画（上表各项的光效、镜球、霓虹、文字等）。
- `skin_maid` 的循环雪花粒子。
- 循环的精灵动画只能显示序列化的那一帧（演奏中没有可见的循环精灵动画）。
- 宽高比 > 16:9 时原作会缩小舞台根并上移，静态背景无法随屏幕比例改变。

## Fever 背景

多数背景 bundle 带有 `liveBG_fever.png`，但单人 live 从不显示它，所以不生成：

- `BgManager` 的初始化（`initCollaboSkin`、`initForMultiNormal` 等）把 `liveBG_normal` 设为背景，仅当 `InGameUtility.IsExistFeverMode()` 为真时才额外加载 `liveBG_fever`。该函数即 `InGameMode == MultiNormal(2) || InGameMode == MultiTeamLiveFestival(5)`（JP 10.1.4 `0x331C3D0`，10.2.0 `0x331B514`，ARM64 切片 `0331b514__InGameUtility__IsExistFeverMode.arm64.tsv`）。
- `BgManager.ChangeBgTextureFever`（`0x387CCE4`）只在 fever 纹理已加载时把背景换成它，即协力 live 与团队 Live 祭典的 Fever 时间（`FeverTimeManager`，由队员的 Fever 点数与 BMS 的 Fever 指令驱动）。
- NotGarupa 复刻单人 live（`SingleNormal(1)` 等），引擎不实现 Fever（Garupa JSON 不产生 Fever 指令），背景始终是 `liveBG_normal`。

Sonolus 中能否在一局内切换背景（留作以后需要时参考）：

- **不能换条目**：背景条目在关卡开始时确定（玩家选择或关卡 `useBackground`），整局不变。运行时只有 `RuntimeBackground`，即 `Background.Get()` / `Set(quad)`：背景图贴在屏幕上的四边形。引擎无法换条目、无法得知玩家选了哪个条目，也就无法用皮肤精灵画出与之对应的另一张图。
- **同一条目内可以换画面**：一个背景条目只有一张 `image`，引擎不能选纹理或裁剪 UV，但 `Set(quad)` 每帧可写（引擎的开场舞台缩放就是这样做的）。若把两张底图上下拼进同一张 image，平时让 quad 显示其中一半，切换时移动 quad 显示另一半，并用皮肤中的纯色精灵盖住在高屏幕或舞台缩放时露出的另一半，即可在关卡内按条件切换。代价是图片体积与显存翻倍，且引擎之外显示整张背景图的地方会看到拼接图，`aspectRatio` / `fit` 也只能按拼接后的尺寸填写。
- 照原作单人 live 没有切换条件，所以不采用；只有将来实现原作没有的单人 Fever 时才需要。

## 命令行

从 GitHub Release 下载对应平台的压缩包（linux / windows / darwin，amd64 / arm64；由 `.github/workflows/build.yml` 矩阵编译，推送 `v*` 标签时发布），解压后直接运行；也可以用 Go 安装或在本仓库中运行（仓库为私有，需要 `GOPRIVATE=github.com/WindowsSov8forUs/*` 和可访问仓库的 git 凭据）：

```sh
go install github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/cmd/sonolus-notgarupa-background-generator@latest
go run ./cmd/sonolus-notgarupa-background-generator   # 在本仓库中
```

```sh
sonolus-notgarupa-background-generator    # 全部条目 → out/source 与 out/pack
```

程序内置所需的 APK 资源（DarkCover 贴图），其余素材联网下载到 `-cache`（默认当前目录的 `.cache/`）。缓存就绪时全量生成约 20 秒，包约 39 MB。

| 参数 | 默认值 | 作用 |
|---|---|---|
| `-out` | `out` | 输出目录：`<out>/source`（重新生成的 sonolus-pack 源目录）与 `<out>/pack`（可直接被服务器加载） |
| `-cache` | `.cache` | 下载缓存目录 |
| `-offline` | false | 只使用缓存中的下载 |
| `-regions` | `jp,en,tw,cn,kr` | Bestdori 服务器优先级。各服有独占素材，每个 bundle 取自第一个拥有它的服务器（该 bundle 的全部文件都来自同一服）；某服没有时记录在缓存中，`-offline` 重跑时选择相同的服务器 |
| `-asset-version` | 固定版本 | 读取舞台特效 prefab 的官方 CDN 资源版本 |
| `-j` | CPU 数 | 并行生成的背景数 |
| `-v` | false | 输出下载日志 |
| `-version` | false | 输出版本号后退出 |

## 作为 Go 库

模块根包 `background` 提供与命令行相同的生成功能，不写文件、不退出进程、不使用 sonolus-pack-go，便于嵌入服务器（服务器启动时一次性生成）：

```go
import background "github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator"

names, _ := background.Items() // 全部条目名，不需要网络
err := background.Generate(ctx, background.Options{
	Source: background.Source{CacheDir: "cache/notgarupa"},
}, func(r *background.Result) error {
	files, err := r.Files()
	if err != nil {
		return err
	}
	it := r.Item // database.DatabaseBackgroundItem，资源字段待填
	it.Thumbnail, it.Data, it.Image, it.Configuration = s.AddBytes(files["thumbnail"], ""), s.AddBytes(files["data"], ""), s.AddBytes(files["image"], ""), s.AddBytes(files["configuration"], "")
	s.Background.Items = append(s.Background.Items, sonolusserver.BackgroundItemModel{DatabaseBackgroundItem: it})
	return nil
})
```

- `Options`：`Source`（`CacheDir`、`Regions`、`AssetVersion`、`Offline`、`HTTPClient`、`Log`）与 `Workers`（0 为 CPU 数）。`Generate` 的 `ctx` 可取消下载，多个生成器可以共用同一个 `CacheDir`（缓存写入是原子的）。
- `Items()` 按生成顺序返回全部条目名。
- `Generate` 按 `Items()` 的顺序逐个把结果交给回调（同一时间只有一个回调在执行），返回第一个错误。每个 `Result`：`Item`（sonolus-core-go 的 `database.DatabaseBackgroundItem`，资源字段留空）、`Habahiro`、Data、Configuration、Image、Thumbnail（PNG）、来源 bundle / 文件、可见的舞台特效部件 `StageEffect`。
- `Result.Files()` 按 item.json 的字段名返回资源字节："thumbnail"、"data"、"configuration"（均已 gzip 压缩）、"image"。
- `background.CreateSourceTree(dir)` 与 `(*SourceTree).Add(r)` 把结果写成 sonolus-pack 源目录（命令行就是 `Generate` + `SourceTree` + sonolus-pack-go）。

公共代码（Bestdori / 官方 CDN 下载与缓存、Unity bundle 解析、图像处理、打包输出、主数据）放在本项目 `internal/` 下，项目可单独构建与运行。资源名称约定见 `sonolus-notgarupa-engine/docs/RESOURCE_CONTRACT.md`。
