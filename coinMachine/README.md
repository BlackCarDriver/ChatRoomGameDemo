# 梭哈模拟器

一个部署在聊天室插件房里的赛博霓虹 3D 投币小游戏。玩法是「押注 → 取出或开奖」：奖池有 50% 概率翻倍、50% 概率清零；同时带有每日赠送、免费次数、银行借贷、每日利息、破产和净资产榜，整体定位是**赌博与债务风险模拟**。

- 3D 机器、金币堆、红绿灯管、7 段 LED、Bloom 霓虹、程序化音效
- PC / 移动端自适应，横屏与竖屏两套布局
- 3D 射线拾取按钮，也可以使用键盘 `空格` / `回车`
- 服务端权威：押注、开奖、贷款、还款、结算全部由 Go 服务端校验
- 零数据库：`data/players.json` 原子写盘 + `.bak` 备份
- 聊天室接入复用 `nichijouSDK`，支持内嵌 iframe 与新标签页票据兑换

## 规则

### 玩法

- 点击绿色按钮投币，金币进入奖池；点击红色按钮开奖。
- 开奖有 50% 概率奖池翻倍，50% 概率奖池清零。
- 随时可以点绿色按钮把奖池取回钱包。
- 一次押注后可以连续开奖；连续开奖不再计数、不再收手续费。

### 次数与手续费

- 每天前 10 次**押注**免手续费；次数按天计算，日结后重置。
- 第 11–19 次手续费 1；第 20–29 次 2；第 30 次起 3，依次类推。
- 手续费从押注额里扣，奖池只进「押注额 − 手续费」；押注最少 = 手续费 + 1。
- 一次押注算一次参与；之后连续开奖不计数。

### 每日赠送

- 每天**第一次进入游戏**赠送 10 金币；不进入不赠送，不累积。
- 跨天需要重新进入（23:59–00:01 服务端会主动断开连接，00:01 后重连）。
- 没有负债时，10 金币直接进钱包。
- 有负债时，10 金币先挂起，23:59:59 结算时抵扣当天利息；多余部分进钱包。

### 贷款与利息

- 可用余额 `A = 钱包 − 欠款`。
- 新贷款 `L` 满足：`L ≤ 100 × A − 欠款`。
  - 例：钱包 10、欠款 0，最多借 1000。
  - 例：钱包 1010、欠款 1000，最多再借 0。
- 每天 23:59:59 结算，日利息 = 欠款的 1%，向上取整到 0.01 金币，**加进欠款本金**。
- 还款金额任意，最多不超过 `min(钱包, 欠款)`。

### 破产

- 23:59:59 结算后，如果 `总资产 < 欠款` 且 `欠款 > 0`，账号破产。
- 总资产 = 钱包 + 奖池；净资产 = 总资产 − 欠款。
- 破产后：不能押注、不能贷款、不能还款；每日 10 金币仍会发放；利息仍会每日累积；无法解冻，直到管理员删除存档。
- 榜单显示「破产」标签。

### 关门与断线

- 23:50 提示准备关门。
- 23:59 强制结束所有奖池并自动提款，断开游戏连接。
- 23:59–00:01 暂停参与，客户端显示「结算中」，00:01 自动重连。
- 断线后 3 秒内重连可以恢复奖池；超过 3 秒自动提款。刷新页面视为断线。
- 服务器重启时，所有未结束的奖池会被强制结算并自动提款，然后写盘。

### 榜单

- 全局账号共享，昵称即账号 ID。
- 按净资产降序；并列时按总资产降序、昵称升序。
- 展示头像、昵称、总资产、欠款、净资产、破产标签。
- 前端显示前 100 名 + 自己。

## 技术栈

| 部分 | 实现 |
|---|---|
| 服务端 | Go 1.21+、`gorilla/websocket`、`github.com/BlackCarDriver/nichijouSDK` |
| 存储 | `data/players.json` 原子写盘（临时文件 + fsync + rename），每 5 分钟刷新 `.bak` |
| 前端 | 原生 HTML/CSS/ES Module + Three.js + `UnrealBloomPass` |
| 构建 | `esbuild` 打包为单文件 `public/app.js`，提交构建产物，部署无需 Node |
| 资源 | 无外部图片/字体/音频；贴图、招牌、LED、音效全部程序化生成 |

## 目录

```text
coinMachine/
├── main.go            # 启动、HTTP、桥接、优雅退出
├── config.go          # 运行配置与游戏参数
├── model.go           # 账号、回合、利息、结算、破产
├── store.go           # JSON 存档、原子写、备份恢复
├── hub.go             # 连接、游戏操作、窗口、桥接状态
├── ws.go              # WebSocket 鉴权与协议
├── leaderboard.go     # 净资产榜
├── view.go            # 下发给前端的视图
├── logfile.go         # 日志自动裁剪
├── *_test.go          # 规则与 hub 测试
├── config/            # integration.json、plugin_config.json、公钥
├── data/              # players.json、players.json.bak（运行时生成）
├── public/            # index.html、style.css、app.js（构建产物）
├── web/               # 前端源码、esbuild 构建脚本
└── shell/             # build.sh、run.sh、stop.sh
```

## 本地运行

### 1. 直接预览页面（不连聊天室）

如果已有实例在运行，先停止：

```bash
./shell/stop.sh
```

然后启动：

```bash
cd coinMachine
go run .
# 浏览器打开
# http://127.0.0.1:8091/?mock=1
```

`?mock=1` 是纯前端本地模拟：不连接服务器，不消耗真实账号，适合调 3D/UI/动画。
`?debug=1` 会显示调试面板，可模拟赢、输、破产、结算中、大数字、切换画质。

### 2. 复用画猜房间联调

本项目默认复用 `drawGuessGame` 的房间和端口：

```json
// config/integration.json（与画猜相同）
{
  "chatroom": "https://101.132.143.148",
  "roomId": 20005,
  "integrationId": "intg_vS_ECUFhqJAfka_6",
  "bridgeSecret": "...",
  "exchangeSecret": "...",
  "secretVersion": 1
}
```

`config/plugin_config.json` 使用 `addr: ":8091"`。同一个房间只能有一条有效桥接连接，所以联调前先停掉画猜：

```bash
cd drawGuessGame
./shell/stop.sh

cd ../coinMachine
./shell/stop.sh   # 如果已有实例
go run .
# 或 ./shell/run.sh
```

然后从聊天室房间 20005 进入游戏。房间的页面地址和 WS 地址已经指向 `:8091`，无需新建房间。

手机联调时，`localhost` 会指向手机自己，需要把聊天室房间的页面地址/WS 地址改成运行游戏机器的局域网 IP，例如：

```text
http://192.168.1.10:8091/
ws://192.168.1.10:8091/ws
```

并确保聊天室允许内网地址。

### 3. 新建房间

如果不想复用画猜房间：

1. 在聊天室管理端创建插件房，承载模板选「内嵌」，画布比例按需要选择。
2. 将管理端生成的接入配置保存到 `config/integration.json`。
3. 下载公钥到 `config/plugin_jwt.pub.pem`。
4. 修改 `config/plugin_config.json` 的 `addr`、`instanceId`、`pubkey`。
5. 启动服务，确认日志出现 `[nichijouSDK] connected` 和身份挑战通过。

## 配置

`config/plugin_config.json`：

| 字段 | 默认 | 说明 |
|---|---|---|
| `addr` | `:8091` | 监听地址 |
| `instanceId` | `coin-1` | 实例标识，仅排障 |
| `pubkey` | `config/plugin_jwt.pub.pem` | 聊天室公钥 |
| `dataDir` | `data` | 存档目录 |
| `timezone` | `Asia/Shanghai` | 日结时区 |
| `dailyGiftCoins` | `10` | 每日赠送金币 |
| `freePlays` | `10` | 每日免费押注次数 |
| `feeCoins` | `1` | 手续费基数；第 11–19 局 1 倍、20–29 局 2 倍、30 局起 3 倍… |
| `interestRateBps` | `100` | 日利率，100 = 1% |
| `loanMultiplier` | `100` | 贷款额度倍数 |
| `revealMs` | `2500` | 开奖动画时长（毫秒） |
| `graceSeconds` | `3` | 断线自动提款宽限（秒） |

优先级：命令行参数 > 环境变量 > `integration.json` > `plugin_config.json` > 内置默认值。

## 存档与重置

- 运行时生成 `data/players.json`；每 5 分钟和优雅退出时刷新 `data/players.json.bak`。
- 主文件损坏时自动回退 `.bak`；两者都损坏则拒绝启动，避免覆盖数据。
- **重置全部账号**：先停服务，删除 `data/players.json` 和 `data/players.json.bak`，再启动。
- `data/players.json.legacy` 是旧原型留下的测试数据，可直接删除。

## 前端构建

```bash
cd web
npm install
npm run build
# 输出 ../public/app.js
```

`shell/build.sh` 会在存在 `web/node_modules` 时自动重新打包前端，然后编译 Linux 二进制：

```bash
./shell/build.sh
./shell/run.sh -addr :8091
./shell/stop.sh
```

## 测试

```bash
go test ./...
```

覆盖：利息、贷款上限、免费次数与手续费、日结赠送抵扣、破产判定、开奖结算、提现、存档往返、榜单排序、hub 押注幂等、破产禁止押注、断线自动提款、窗口阶段、日结幂等。

## 免责声明

本游戏使用纯虚拟金币，不涉及真实货币充值、提现或兑换。内容为赌博与债务风险模拟，请理性参与，警惕债务。
