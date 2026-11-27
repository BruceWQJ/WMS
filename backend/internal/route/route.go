// Package route 实现 WMS 拣料动线的寻优算法引擎。
//
// 模块职责：在仓库二维网格上，依据库位存储类型与单行道通行方向构建
// 受限通行图，先以 BFS 求解任意两点间的最短合法路径，再对一组拣料点
// 做访问顺序优化（TSP），最终返回从起点出发、依次访问全部拣料点并
// 返回终点的完整路径、拣料点顺序与总步数。
//
// 通行约束：
//   - RACK（高架区）为单行道，只允许沿配置的南北向方向通行，禁止逆行；
//   - FLOOR（地堆区）默认可双向通行，也可配置为单向；
//   - 所有移动均不能越出仓库网格边界。
package route

import (
	"fmt"

	"wms/backend/internal/model"
)

// 通行方向枚举。网格移动方向由位移增量 (dx, dy) 推导得到。
const (
	// DirBidirectional 双向通行，不限定方向。
	DirBidirectional = "BIDIRECTIONAL"
	// DirNorthToSouth 自北向南（y 增大方向，对应屏幕向下）。
	DirNorthToSouth = "NORTH_TO_SOUTH"
	// DirSouthToNorth 自南向北（y 减小方向，对应屏幕向上）。
	DirSouthToNorth = "SOUTH_TO_NORTH"
	// DirEastToWest 自东向西（x 减小方向，对应屏幕向左）。
	DirEastToWest = "EAST_TO_WEST"
	// DirWestToEast 自西向东（x 增大方向，对应屏幕向右）。
	DirWestToEast = "WEST_TO_EAST"
)

// infCost 作为不可达 / 超大代价的上界，TSP 代价均不得超过该值。
const infCost = 1 << 30

// gridCell 描述单个网格格点的存储类型与通行方向。
type gridCell struct {
	storageType string
	direction   string
}

// grid 是仓库网格的内存表示，cells 按行优先一维存储，索引由 idx 计算。
type grid struct {
	width  int
	height int
	cells  []gridCell
}

// newGrid 依据仓库尺寸与库位列表构建网格。未显式配置的格点按
// FLOOR 地堆区、双向通行处理；超出边界的库位被忽略。
func newGrid(wh model.Warehouse, locs []model.Location) *grid {
	g := &grid{width: wh.Width, height: wh.Height, cells: make([]gridCell, wh.Width*wh.Height)}
	for i := range g.cells {
		g.cells[i] = gridCell{storageType: "FLOOR", direction: DirBidirectional}
	}
	for _, l := range locs {
		if !g.inBounds(l.X, l.Y) {
			continue
		}
		dir := l.Direction
		if dir == "" {
			dir = DirBidirectional
		}
		g.cells[g.idx(l.X, l.Y)] = gridCell{storageType: l.StorageType, direction: dir}
	}
	return g
}

// idx 将二维坐标转换为 cells 的一维行优先索引。
func (g *grid) idx(x, y int) int { return y*g.width + x }

// inBounds 判断坐标是否落在仓库网格范围内。
func (g *grid) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < g.width && y < g.height
}

// allowedOut 判断从格点 c 沿方向 dir 离开是否合法。
// 高架区只允许沿其配置的南北向单向离开；双向格点任意方向均可；
// 其余单向格点必须方向一致。
func (g *grid) allowedOut(c gridCell, dir string) bool {
	switch c.storageType {
	case "RACK":
		return c.direction == dir && (dir == DirNorthToSouth || dir == DirSouthToNorth)
	default:
		if c.direction == "" || c.direction == DirBidirectional {
			return true
		}
		return c.direction == dir
	}
}

// moveDir 根据位移增量返回对应的通行方向枚举。
func moveDir(dx, dy int) string {
	switch {
	case dy == 1:
		return DirNorthToSouth
	case dy == -1:
		return DirSouthToNorth
	case dx == -1:
		return DirEastToWest
	case dx == 1:
		return DirWestToEast
	}
	return ""
}

// canMove 尝试从 (x, y) 按增量 (dx, dy) 移动到相邻格点。
// 同时校验目标格点在边界内、起点与终点格点均允许该方向通行；
// 成功时返回新坐标与 true，否则返回 false。
func (g *grid) canMove(x, y, dx, dy int) (int, int, bool) {
	nx, ny := x+dx, y+dy
	if !g.inBounds(nx, ny) {
		return 0, 0, false
	}
	d := moveDir(dx, dy)
	from := g.cells[g.idx(x, y)]
	to := g.cells[g.idx(nx, ny)]
	if !g.allowedOut(from, d) || !g.allowedOut(to, d) {
		return 0, 0, false
	}
	return nx, ny, true
}

// bfsResult 保存一次 BFS 的距离、父节点指针与访问标记，用于路径回溯。
type bfsResult struct {
	dist    []int
	parent  []int
	visited []bool
}

// bfs 从 (sx, sy) 出发，在单行道约束下做广度优先搜索，
// 得到到各可达格点的最短步数与父节点链。不可达格点 dist 记为 -1。
func (g *grid) bfs(sx, sy int) *bfsResult {
	n := len(g.cells)
	res := &bfsResult{
		dist:    make([]int, n),
		parent:  make([]int, n),
		visited: make([]bool, n),
	}
	for i := range res.dist {
		res.dist[i] = -1
		res.parent[i] = -1
	}
	s := g.idx(sx, sy)
	res.dist[s] = 0
	res.visited[s] = true
	queue := make([]int, 0, n)
	queue = append(queue, s)
	// 上下左右四个相邻方向。
	dirs := [4][2]int{{0, 1}, {0, -1}, {-1, 0}, {1, 0}}
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		cx, cy := cur%g.width, cur/g.width
		for _, d := range dirs {
			nx, ny, ok := g.canMove(cx, cy, d[0], d[1])
			if !ok {
				continue
			}
			ni := g.idx(nx, ny)
			if res.visited[ni] {
				continue
			}
			res.visited[ni] = true
			res.dist[ni] = res.dist[cur] + 1
			res.parent[ni] = cur
			queue = append(queue, ni)
		}
	}
	return res
}

// pathFrom 依据 BFS 父节点链回溯出从起点到 (tx, ty) 的坐标路径。
// 目标越界或不可达时返回 false；返回路径包含起点与终点。
func (g *grid) pathFrom(res *bfsResult, tx, ty int) ([]model.Point, bool) {
	if !g.inBounds(tx, ty) {
		return nil, false
	}
	ti := g.idx(tx, ty)
	if !res.visited[ti] {
		return nil, false
	}
	// 沿父节点从终点反向收集，最后反转得到正向路径。
	rev := []model.Point{}
	for cur := ti; cur != -1; cur = res.parent[cur] {
		rev = append(rev, model.Point{X: cur % g.width, Y: cur / g.width})
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, true
}

// OptimizeRoute 在单行道约束下，计算从起点出发、依次访问所有拣料点并
// 返回终点的最优动线。
//
// 参数：
//   - wh：仓库配置（网格尺寸、起点 / 终点坐标）；
//   - locs：全部库位及其类型、方向、优先级属性；
//   - stopPts：本次订单需要访问的拣料点坐标（可能重复或越界，内部去重过滤）。
//
// 返回 model.Route，其中 Path 为完整坐标路径，Stops 为按访问顺序排列的
// 拣料点，Distance 为总步数；Reachable 为 false 时 Message 说明不可达原因。
func OptimizeRoute(wh model.Warehouse, locs []model.Location, stopPts []model.Point) model.Route {
	g := newGrid(wh, locs)
	start := model.Point{X: wh.StartX, Y: wh.StartY}
	end := model.Point{X: wh.EndX, Y: wh.EndY}
	if !g.inBounds(start.X, start.Y) {
		return model.Route{Reachable: false, Message: "起点超出仓库范围", Path: []model.Point{}}
	}
	if !g.inBounds(end.X, end.Y) {
		return model.Route{Reachable: false, Message: "终点超出仓库范围", Path: []model.Point{}}
	}

	// 拣料点去重并过滤越界点，stops 为待访问的唯一拣料点集合。
	seen := map[[2]int]bool{}
	stops := make([]model.Point, 0, len(stopPts))
	for _, p := range stopPts {
		if !g.inBounds(p.X, p.Y) {
			continue
		}
		key := [2]int{p.X, p.Y}
		if seen[key] {
			continue
		}
		seen[key] = true
		stops = append(stops, p)
	}
	n := len(stops)

	// 对起点与每个拣料点分别做一次 BFS；bfsNodes[0] 对应起点，
	// bfsNodes[i+1] 对应 stops[i]，供后续计算两两最短路。
	bfsNodes := make([]*bfsResult, n+1)
	bfsNodes[0] = g.bfs(start.X, start.Y)
	for i, p := range stops {
		bfsNodes[i+1] = g.bfs(p.X, p.Y)
	}

	// dist[i][j] 表示节点 i 到节点 j 的最短步数；节点 0 为起点，
	// 节点 i+1 为 stops[i]。不可达记为 -1。
	dist := make([][]int, n+1)
	for i := range dist {
		dist[i] = make([]int, n+1)
	}
	for i := 0; i <= n; i++ {
		for j := 0; j <= n; j++ {
			var tgt model.Point
			if j == 0 {
				tgt = start
			} else {
				tgt = stops[j-1]
			}
			ti := g.idx(tgt.X, tgt.Y)
			if bfsNodes[i].visited[ti] {
				dist[i][j] = bfsNodes[i].dist[ti]
			} else {
				dist[i][j] = -1
			}
		}
	}

	// endDist[i] 为节点 i 直接返回终点的最短步数，不可达记为 -1。
	endDist := make([]int, n+1)
	endIdx := g.idx(end.X, end.Y)
	for i := 0; i <= n; i++ {
		if bfsNodes[i].visited[endIdx] {
			endDist[i] = bfsNodes[i].dist[endIdx]
		} else {
			endDist[i] = -1
		}
	}

	// 没有拣料点时，只需直接走起点到终点。
	if n == 0 {
		path := []model.Point{start}
		distance := 0
		if end != start {
			p, ok := g.pathFrom(bfsNodes[0], end.X, end.Y)
			if !ok {
				return model.Route{Reachable: false, Message: "起点与终点之间不可达", Path: []model.Point{start}, Stops: []model.Point{}}
			}
			path = p
			distance = len(p) - 1
		}
		return model.Route{Path: path, Stops: []model.Point{}, Distance: distance, Reachable: true}
	}

	// 拣料点较少（≤14）时用 Held-Karp 精确求解 TSP；
	// 较多时用贪心构造初始解，再做局部改进以控制计算量。
	var order []int
	reachable := true
	if n <= 14 {
		order, reachable = heldKarp(dist, endDist, n)
	} else {
		order, reachable = greedyOrder(dist, endDist, n)
	}
	if !reachable {
		return model.Route{
			Reachable: false,
			Message:   "受单行道约束，部分拣料库位不可达，无法生成完整动线",
			Path:      []model.Point{start},
			Stops:     []model.Point{},
		}
	}

	// 按求解出的访问顺序，逐段用 BFS 父节点链拼出完整路径；
	// 拼接时下一段跳过重复的起点格点。
	path := []model.Point{start}
	appendSegment := func(fromIdx int, to model.Point) bool {
		seg, ok := g.pathFrom(bfsNodes[fromIdx], to.X, to.Y)
		if !ok {
			return false
		}
		if len(seg) > 0 {
			path = append(path, seg[1:]...)
		}
		return true
	}

	prevIdx := 0
	for _, sIdx := range order {
		if !appendSegment(prevIdx, stops[sIdx]) {
			return model.Route{Reachable: false, Message: "动线路径重建失败", Path: []model.Point{start}, Stops: []model.Point{}}
		}
		prevIdx = sIdx + 1
	}
	// 最后一段由末个拣料点返回终点。
	if !appendSegment(prevIdx, end) {
		return model.Route{Reachable: false, Message: "返回终点失败", Path: []model.Point{start}, Stops: []model.Point{}}
	}

	orderedStops := make([]model.Point, 0, n)
	for _, sIdx := range order {
		orderedStops = append(orderedStops, stops[sIdx])
	}

	return model.Route{
		Path:      path,
		Stops:     orderedStops,
		Distance:  len(path) - 1,
		Reachable: true,
	}
}

// heldKarp 使用动态规划（Held-Karp）精确求解小规模 TSP。
//
// dp[mask][last]：已访问集合为 mask、末个访问点为 last 时的最小代价；
// par 记录前驱点用于回溯访问顺序。返回拣料点索引序列（stops 下标）与
// 是否可达；存在不可达点时返回 false。
func heldKarp(dist [][]int, endDist []int, n int) ([]int, bool) {
	full := 1 << n
	dp := make([][]int, full)
	par := make([][]int, full)
	for mask := 0; mask < full; mask++ {
		dp[mask] = make([]int, n)
		par[mask] = make([]int, n)
		for i := 0; i < n; i++ {
			dp[mask][i] = infCost
			par[mask][i] = -1
		}
	}
	// 初始化：从起点直接到达单个拣料点。
	for i := 0; i < n; i++ {
		d := dist[0][i+1]
		if d < 0 {
			continue
		}
		dp[1<<i][i] = d
	}
	// 枚举已访问集合与末点，尝试扩展到未访问的下一点。
	for mask := 1; mask < full; mask++ {
		for last := 0; last < n; last++ {
			if dp[mask][last] >= infCost {
				continue
			}
			for next := 0; next < n; next++ {
				if mask&(1<<next) != 0 {
					continue
				}
				d := dist[last+1][next+1]
				if d < 0 {
					continue
				}
				nm := mask | (1 << next)
				cost := dp[mask][last] + d
				if cost < dp[nm][next] {
					dp[nm][next] = cost
					par[nm][next] = last
				}
			}
		}
	}
	// 选择总代价最小的末点，并加上其返回终点的代价。
	bestCost := infCost
	bestLast := -1
	for i := 0; i < n; i++ {
		if dp[full-1][i] >= infCost || endDist[i+1] < 0 {
			continue
		}
		cost := dp[full-1][i] + endDist[i+1]
		if cost < bestCost {
			bestCost = cost
			bestLast = i
		}
	}
	if bestLast == -1 {
		return nil, false
	}
	// 沿前驱指针回溯，得到反向顺序后再反转。
	order := make([]int, 0, n)
	mask := full - 1
	cur := bestLast
	for cur != -1 {
		order = append(order, cur)
		prev := par[mask][cur]
		mask &^= 1 << cur
		cur = prev
	}
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	return order, true
}

// tourCost 计算给定访问顺序的完整路线代价（起点→各拣料点→终点）。
// 顺序中存在不可达段时返回 infCost。
func tourCost(order []int, dist [][]int, endDist []int) int {
	if len(order) == 0 {
		return dist[0][0]
	}
	cost := dist[0][order[0]+1]
	if cost < 0 {
		return infCost
	}
	for i := 0; i+1 < len(order); i++ {
		d := dist[order[i]+1][order[i+1]+1]
		if d < 0 {
			return infCost
		}
		cost += d
	}
	d := endDist[order[len(order)-1]+1]
	if d < 0 {
		return infCost
	}
	return cost + d
}

// moveOrder 将第 from 个点移动到第 to 个位置，返回新顺序（不改原切片）。
func moveOrder(order []int, from, to int) []int {
	cand := append([]int(nil), order...)
	value := cand[from]
	cand = append(cand[:from], cand[from+1:]...)
	if to > from {
		to--
	}
	cand = append(cand, 0)
	copy(cand[to+1:], cand[to:])
	cand[to] = value
	return cand
}

// swapOrder 交换顺序中第 i、j 个点，返回新顺序。
func swapOrder(order []int, i, j int) []int {
	cand := append([]int(nil), order...)
	cand[i], cand[j] = cand[j], cand[i]
	return cand
}

// reverseOrder 反转顺序中下标 [i, j] 区间内的点，返回新顺序。
func reverseOrder(order []int, i, j int) []int {
	cand := append([]int(nil), order...)
	for i < j {
		cand[i], cand[j] = cand[j], cand[i]
		i++
		j--
	}
	return cand
}

// improveOrder 对初始访问顺序做局部搜索改进：依次尝试重定位（move）、
// 交换（swap）、区间反转（2-opt 式 reverse）三类邻域，只要代价下降
// 就接受并继续迭代，直到没有可改进的邻域为止。
// 拣料点超过 60 个时跳过改进，避免局部搜索耗时过长。
func improveOrder(order []int, dist [][]int, endDist []int) []int {
	best := append([]int(nil), order...)
	if len(best) > 60 {
		return best
	}
	bestCost := tourCost(best, dist, endDist)
	if bestCost >= infCost {
		return best
	}
	for {
		improved := false
		n := len(best)
		// 邻域一：单点重定位到另一位置。
		for from := 0; from < n && !improved; from++ {
			for to := 0; to < n; to++ {
				if from == to {
					continue
				}
				cand := moveOrder(best, from, to)
				cost := tourCost(cand, dist, endDist)
				if cost < bestCost {
					best, bestCost, improved = cand, cost, true
					break
				}
			}
		}
		if improved {
			continue
		}
		// 邻域二：交换两个点。
		for i := 0; i < n && !improved; i++ {
			for j := i + 1; j < n; j++ {
				cand := swapOrder(best, i, j)
				cost := tourCost(cand, dist, endDist)
				if cost < bestCost {
					best, bestCost, improved = cand, cost, true
					break
				}
			}
		}
		if improved {
			continue
		}
		// 邻域三：反转一段区间。
		for i := 0; i < n && !improved; i++ {
			for j := i + 2; j < n; j++ {
				cand := reverseOrder(best, i, j)
				cost := tourCost(cand, dist, endDist)
				if cost < bestCost {
					best, bestCost, improved = cand, cost, true
					break
				}
			}
		}
		if !improved {
			return best
		}
	}
}

// greedyOrder 以每个点作为起点做最近邻贪心，取全局最优的贪心顺序，
// 再交给 improveOrder 做局部改进。全部起点均不可行时返回 false。
func greedyOrder(dist [][]int, endDist []int, n int) ([]int, bool) {
	best := []int(nil)
	bestCost := infCost
	for first := 0; first < n; first++ {
		if dist[0][first+1] < 0 {
			continue
		}
		visited := make([]bool, n)
		order := []int{first}
		visited[first] = true
		cur := first + 1
		// 每次从未访问点中选择距当前点最近的一个。
		for len(order) < n {
			next := -1
			bestD := infCost
			for j := 0; j < n; j++ {
				if visited[j] {
					continue
				}
				d := dist[cur][j+1]
				if d >= 0 && d < bestD {
					bestD = d
					next = j
				}
			}
			if next == -1 {
				break
			}
			visited[next] = true
			order = append(order, next)
			cur = next + 1
		}
		if len(order) < n {
			continue
		}
		cost := tourCost(order, dist, endDist)
		if cost < bestCost {
			bestCost = cost
			best = order
		}
	}
	if best == nil {
		return nil, false
	}
	return improveOrder(best, dist, endDist), true
}

// describeRoute 生成动线的一句话摘要描述，不可达时返回原因信息。
func describeRoute(r model.Route) string {
	if !r.Reachable {
		return r.Message
	}
	return fmt.Sprintf("访问 %d 个库位，总步数 %d", len(r.Stops), r.Distance)
}
