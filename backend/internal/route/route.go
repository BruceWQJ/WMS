package route

import (
	"fmt"

	"wms/backend/internal/model"
)

const (
	DirBidirectional = "BIDIRECTIONAL"
	DirNorthToSouth  = "NORTH_TO_SOUTH"
	DirSouthToNorth  = "SOUTH_TO_NORTH"
	DirEastToWest    = "EAST_TO_WEST"
	DirWestToEast    = "WEST_TO_EAST"
)

const infCost = 1 << 30

type gridCell struct {
	storageType string
	direction   string
}

type grid struct {
	width  int
	height int
	cells  []gridCell
}

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

func (g *grid) idx(x, y int) int { return y*g.width + x }
func (g *grid) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < g.width && y < g.height
}

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

type bfsResult struct {
	dist    []int
	parent  []int
	visited []bool
}

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

func (g *grid) pathFrom(res *bfsResult, tx, ty int) ([]model.Point, bool) {
	if !g.inBounds(tx, ty) {
		return nil, false
	}
	ti := g.idx(tx, ty)
	if !res.visited[ti] {
		return nil, false
	}
	rev := []model.Point{}
	for cur := ti; cur != -1; cur = res.parent[cur] {
		rev = append(rev, model.Point{X: cur % g.width, Y: cur / g.width})
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, true
}

// OptimizeRoute 在单行道约束下，计算从起点出发、依次访问所有拣料点并返回终点的最优动线。
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

	bfsNodes := make([]*bfsResult, n+1)
	bfsNodes[0] = g.bfs(start.X, start.Y)
	for i, p := range stops {
		bfsNodes[i+1] = g.bfs(p.X, p.Y)
	}

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

	endDist := make([]int, n+1)
	endIdx := g.idx(end.X, end.Y)
	for i := 0; i <= n; i++ {
		if bfsNodes[i].visited[endIdx] {
			endDist[i] = bfsNodes[i].dist[endIdx]
		} else {
			endDist[i] = -1
		}
	}

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
	for i := 0; i < n; i++ {
		d := dist[0][i+1]
		if d < 0 {
			continue
		}
		dp[1<<i][i] = d
	}
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

func swapOrder(order []int, i, j int) []int {
	cand := append([]int(nil), order...)
	cand[i], cand[j] = cand[j], cand[i]
	return cand
}

func reverseOrder(order []int, i, j int) []int {
	cand := append([]int(nil), order...)
	for i < j {
		cand[i], cand[j] = cand[j], cand[i]
		i++
		j--
	}
	return cand
}

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

func describeRoute(r model.Route) string {
	if !r.Reachable {
		return r.Message
	}
	return fmt.Sprintf("访问 %d 个库位，总步数 %d", len(r.Stops), r.Distance)
}
