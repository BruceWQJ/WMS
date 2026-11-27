# WMS · 智慧仓库库位建模与拣料动线优化

一个仓库管理系统（WMS）Web 应用原型，实现：仓库网格与库位属性配置、基于拣料优先级的库存自动分配，以及受单行道约束的拣料动线生成与 2D 可视化演示。

- 后端：Go（标准库 `net/http`）+ SQLite（`modernc.org/sqlite`，纯 Go 驱动，无需安装原生数据库）
- 前端：Vue 3 + Vite 7
- 数据存储：SQLite 单文件数据库，首次启动自动建表并写入示例仓库

---
## 一、目录结构

```
WMS/
├── backend/                          # Go 后端服务
│   ├── main.go                       # 入口：加载配置、初始化 DB、种子数据、启动 HTTP 服务
│   ├── go.mod / go.sum
│   ├── wms.db                        # SQLite 数据库文件（运行后自动生成）
│   └── internal/                     # 后端内部分包（不对外暴露）
│       ├── config/config.go          # 配置项（环境变量读取）
│       ├── model/model.go            # 数据结构与请求/响应模型
│       ├── store/store.go            # SQLite 连接、建表迁移、仓库/库位/库存数据访问与示例种子
│       ├── route/route.go            # 网格建模、单行道约束 BFS、动线优化（Held-Karp / 贪心 + 局部改进）
│       ├── service/order.go          # 下单、优先级扣减分配、订单查询
│       └── httpapi/api.go            # REST API 路由、HTTP 处理器与前端静态文件托管
├── frontend/                 # Vue 3 前端
│   ├── index.html
│   ├── vite.config.js         # 开发端口 5173，/api 代理到 127.0.0.1:8080
│   ├── package.json
│   ├── dist/                  # 前端构建产物（npm run build 后生成）
│   └── src/
│       ├── main.js
│       ├── App.vue
│       ├── api.js             # 前端 API 封装
│       ├── style.css
│       └── components/
│           ├── WarehouseMap.vue   # 仓库网格地图与路线渲染
│           ├── OrderPanel.vue     # 订单提交、结果明细、动线播放、历史订单
│           └── CellEditor.vue     # 单个库位属性与库存编辑
└── README.md
```

---

## 二、环境要求

| 组件 | 版本要求 | 说明 |
|---|---|---|
| Go | 1.21 及以上（`go.mod` 声明 1.27.2） | 编译运行后端 |
| Node.js | 20 及以上（Vite 7 要求） | 安装依赖、构建前端 |
| 包管理 | npm（随 Node 自带） | 前端依赖安装 |

> 后端使用纯 Go 实现的 SQLite 驱动，**不需要**单独安装 MySQL/PostgreSQL/SQLite 服务。
> 
---

## 三、快速启动

提供两种运行方式：**开发模式**（前后端分离，支持热更新）和**生产模式**（后端直接托管前端构建产物，单端口访问）。

### 方式 A：开发模式（推荐调试使用）

前后端分别启动，前端 Vite 开发服务器通过代理转发 `/api` 请求到后端。

**1. 启动后端（端口 8080）**

```powershell
cd backend
go run .
```

启动成功后日志显示：`WMS 服务已启动: http://localhost:8080`

**2. 安装前端依赖并启动开发服务器（端口 5173）**

另开一个终端：

```powershell
cd frontend
npm install
npm run dev
```

访问浏览器：<http://localhost:5173>

- 前端页面运行在 5173；
- 所有 `/api` 请求由 Vite 代理到 `http://127.0.0.1:8080`，无需额外配置跨域。

### 方式 B：生产模式（单端口部署）

先把前端打包成静态文件，再由后端直接托管。

**1. 构建前端**

```powershell
cd frontend
npm install
npm run build
```

产物输出到 `frontend/dist/`（包含 `index.html` 与 `assets/`）。

**2. 启动后端**

```powershell
cd backend
go run .
# 或编译为可执行文件后运行：
# go build -o backend.exe .
# .\backend.exe
```

后端默认从 `../frontend/dist` 读取前端静态文件，直接访问：<http://localhost:8080>

> 如果前端产物放在其他目录，可通过环境变量 `WMS_STATIC_DIR` 指定，见下一节。

---

## 四、配置项（环境变量）

后端在 `backend/internal/config/config.go` 中读取以下环境变量，均有默认值，可不配置直接运行。

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `WMS_HTTP_ADDR` | `:8080` | 后端监听地址与端口 |
| `WMS_SQLITE_PATH` | `wms.db` | SQLite 数据库文件路径（相对路径基于后端运行目录） |
| `WMS_STATIC_DIR` | `../frontend/dist` | 前端构建产物目录，用于生产模式托管静态页面 |

Windows PowerShell 示例（临时设置当前终端会话）：

```powershell
$env:WMS_HTTP_ADDR=":9000"
$env:WMS_SQLITE_PATH="D:\data\wms.db"
$env:WMS_STATIC_DIR="D:\app\frontend\dist"
go run .
```

---

## 五、使用说明

界面分为左侧**仓库网格地图**和右侧**操作面板**两部分。

### 1. 仓库配置

- 在右侧「仓库配置」中可修改仓库名称、宽度 X、高度 Y（范围 2–60）。
- 出入口固定为起点 / 终点 `(0, 0)`，点击「保存配置」生效。
- 「重新生成示例」按钮会清空当前库位与订单，重新写入内置示例仓库（20 × 20）。

### 2. 查看与编辑库位

- 地图上每个格子代表一个库位，点击任意格子打开「库位编辑」面板。
- 可配置以下属性：
  - **存储类型**
    - `FLOOR` 地堆区：默认双向通行，优先级高（示例为 10）；也可配置为单向通行。
    - `RACK` 高架区：强制单行道，方向只能为 `NORTH_TO_SOUTH`（↓）或 `SOUTH_TO_NORTH`（↑），优先级低（示例为 5）。
  - **巷道编号** `aisleId`：如 `A04`。
  - **通行方向** `direction`：双向 `BIDIRECTIONAL` 或各单向方向。
  - **拣料优先级** `pickingPriority`：整数，数值越大越优先被扣减。
  - **库存**：若干条 SKU 与数量（如 `SKU_A: 20`）。
- 保存后库位与库存即时写入数据库；已有库位可在此面板删除。

### 3. 生成拣料单

1. 在「交互式订单」面板中：
   - 点击上方 SKU 按钮快捷勾选，或在明细行手动输入 SKU 名称与正整数数量；
   - 可「+ 添加明细」增加多行，点 `×` 删除某一行。
2. 点击「**生成拣料单**」提交订单。
3. 后端按**拣料优先级从高到低**依次扣减库存；高优先级库位不足时，自动向低优先级库位顺序扣减。
4. 结果区展示：
   - 订单状态（`ALLOCATED` 全部分配 / `PARTIAL` 部分缺货）；
   - 总路径步数、拣料点数量；
   - 明细表格：从哪个库位 `(x, y)` 扣减了哪个 SKU、多少数量、对应优先级；
   - 若库存不足，列出缺货 SKU 的需求数量与可分配数量。

### 4. 动线播放与分步高亮

生成拣料单后，地图上会绘制优化后的拣料路线（橙色连线 + 方向箭头），编号圆圈标记各拣料点。

- **播放动线**：沿路径逐步高亮当前所在位置；
- **上一步 / 下一步**：手动单步浏览；
- **重置**：回到路径起点；
- **速度**：可选慢速 / 正常 / 快速。

动线算法在单行道约束下求解：高架巷道内禁止逆行，只能沿合法方向由入口进入、出口离开；访问点较少时使用精确求解（Held-Karp），较多时使用贪心构造加局部改进，并计算返回终点的总路径长度。

### 5. 历史订单

底部「历史订单」列出最近 50 笔订单，点击任意一条可回看其扣减明细与拣料动线。

---

## 六、后端 API 一览

基础前缀：`/api`，请求与响应均为 JSON。

### 健康检查

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 服务健康检查 |

### 仓库

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/warehouse` | 获取仓库配置 |
| POST | `/api/warehouse` | 新建仓库（已存在时返回提示） |
| PUT | `/api/warehouse` | 更新仓库配置 |
| DELETE | `/api/warehouse` | 重置仓库为默认 20×20 配置 |

### 库位

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/locations` | 库位列表（含库存） |
| GET | `/api/locations/{id}` | 查询单个库位 |
| POST | `/api/locations` | 新建库位（同坐标覆盖更新） |
| PUT | `/api/locations/{id}` | 更新库位 |
| DELETE | `/api/locations/{id}` | 删除库位 |

### 库存

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/inventory` | 库存明细列表 |
| POST | `/api/inventory` | 写入 / 覆盖某库位某 SKU 数量 |

### 订单与种子

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/seed` | 清空并重新生成示例仓库数据 |
| GET | `/api/orders` | 历史订单摘要列表（最近 50 条） |
| POST | `/api/orders` | 提交拣料单，返回分配结果与动线 |
| GET | `/api/orders/{id}` | 查询指定订单详情（含分配与动线） |

**下单请求示例**（`POST /api/orders`）：

```json
{
  "items": [
    { "sku": "SKU_A", "qty": 10 },
    { "sku": "SKU_B", "qty": 5 }
  ]
}
```

---

## 七、示例数据与数据库

- 后端首次启动时，若库位表与订单表均为空，会自动写入内置示例仓库（20 × 20）：
  - 地堆区分布在两侧与起点附近，优先级 10；
  - 高架巷道位于指定列，南北向交替单行通行，优先级 5；
  - 预置 `SKU_A` ~ `SKU_F` 若干库存。
- 数据库文件默认位于后端运行目录下的 `wms.db`（含 `-wal`、`-shm` 伴随文件）。
- 删除 `wms.db` 后重启后端即可恢复到初始状态；也可调用 `POST /api/seed` 或在界面点击「重新生成示例」。

---

## 八、常见问题

**Q：前端访问 5173 后接口报错 / 数据加载失败？**
A：请确认后端已在 8080 启动；开发模式下 `/api` 由 Vite 代理到 `127.0.0.1:8080`，后端未启动时无法获取数据。

**Q：生产模式访问 8080 提示「前端尚未构建」或 404？**
A：先在 `frontend` 目录执行 `npm run build` 生成 `dist/`；若产物目录不在默认位置，设置 `WMS_STATIC_DIR` 指向该目录。

**Q：修改了 Go 代码如何生效？**
A：开发模式下 `go run .` 会重新编译；如使用编译产物，需重新 `go build`。

**Q：端口被占用如何修改？**
A：设置环境变量 `WMS_HTTP_ADDR`（后端）；前端开发端口在 `frontend/vite.config.js` 中修改 `server.port`，并同步调整代理目标。
#   W Q J 
 
 #   W M S 
 
 
