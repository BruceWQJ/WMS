// service 实现下单业务：优先级扣减分配、动线生成与订单查询。
package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"wms/backend/internal/model"
	"wms/backend/internal/route"
	"wms/backend/internal/store"
)

// invCandidate 是某 SKU 在可扣减库位上的候选记录。
type invCandidate struct {
	inventoryID int64
	locationID  int64
	x           int
	y           int
	priority    int
	quantity    int
}

// deduction 记录一笔库存扣减（库存行 ID 与扣减数量）。
type deduction struct {
	inventoryID int64
	take        int
}

// absInt 返回整数绝对值。
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// CreateOrder 按拣料优先级扣减库存，并生成受单行道约束的拣料动线。
func CreateOrder(db *sql.DB, req model.CreateOrderRequest, wh model.Warehouse) (model.OrderResponse, error) {
	if len(req.Items) == 0 {
		return model.OrderResponse{}, errors.New("订单明细不能为空")
	}

	merged := map[string]int{}
	order := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		if it.SKU == "" {
			return model.OrderResponse{}, errors.New("SKU 不能为空")
		}
		if it.Qty <= 0 {
			return model.OrderResponse{}, errors.New("拣料数量必须大于 0")
		}
		if _, ok := merged[it.SKU]; !ok {
			order = append(order, it.SKU)
		}
		merged[it.SKU] += it.Qty
	}

	tx, err := db.Begin()
	if err != nil {
		return model.OrderResponse{}, err
	}
	defer tx.Rollback()

	allocs := []model.Allocation{}
	shortages := []model.Shortage{}
	deductions := []deduction{}

	for _, sku := range order {
		need := merged[sku]
		rows, err := tx.Query(`SELECT i.id, i.location_id, l.x, l.y, l.picking_priority, i.quantity
			FROM inventory i JOIN location l ON l.id = i.location_id
			WHERE i.sku = ? AND i.quantity > 0`, sku)
		if err != nil {
			return model.OrderResponse{}, err
		}
		cands := []invCandidate{}
		for rows.Next() {
			var c invCandidate
			if err := rows.Scan(&c.inventoryID, &c.locationID, &c.x, &c.y, &c.priority, &c.quantity); err != nil {
				rows.Close()
				return model.OrderResponse{}, err
			}
			if c.x < 0 || c.y < 0 || c.x >= wh.Width || c.y >= wh.Height {
				continue
			}
			cands = append(cands, c)
		}
		rows.Close()

		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].priority != cands[j].priority {
				return cands[i].priority > cands[j].priority
			}
			di := absInt(cands[i].x-wh.StartX) + absInt(cands[i].y-wh.StartY)
			dj := absInt(cands[j].x-wh.StartX) + absInt(cands[j].y-wh.StartY)
			if di != dj {
				return di < dj
			}
			return cands[i].locationID < cands[j].locationID
		})

		allocated := 0
		for _, c := range cands {
			if need <= 0 {
				break
			}
			take := c.quantity
			if take > need {
				take = need
			}
			if take <= 0 {
				continue
			}
			allocs = append(allocs, model.Allocation{
				LocationID: c.locationID,
				X:          c.x,
				Y:          c.y,
				SKU:        sku,
				Qty:        take,
				Priority:   c.priority,
			})
			deductions = append(deductions, deduction{inventoryID: c.inventoryID, take: take})
			need -= take
			allocated += take
		}
		if allocated < merged[sku] {
			shortages = append(shortages, model.Shortage{SKU: sku, Requested: merged[sku], Allocated: allocated})
		}
	}

	locs, err := store.ListLocationAttrs(tx)
	if err != nil {
		return model.OrderResponse{}, err
	}
	stopPts := make([]model.Point, 0, len(allocs))
	for _, a := range allocs {
		stopPts = append(stopPts, model.Point{X: a.X, Y: a.Y})
	}
	rt := route.OptimizeRoute(wh, locs, stopPts)
	if !rt.Reachable {
		return model.OrderResponse{}, fmt.Errorf("无法生成合法拣料动线：%s", rt.Message)
	}

	status := "ALLOCATED"
	if len(shortages) > 0 {
		status = "PARTIAL"
	}
	routeJSON, _ := json.Marshal(rt)
	res, err := tx.Exec(`INSERT INTO orders (status, total_distance, route_json) VALUES (?, ?, ?)`,
		status, rt.Distance, string(routeJSON))
	if err != nil {
		return model.OrderResponse{}, err
	}
	orderID, _ := res.LastInsertId()

	for _, sku := range order {
		if _, err := tx.Exec(`INSERT INTO order_items (order_id, sku, requested_qty) VALUES (?, ?, ?)`,
			orderID, sku, merged[sku]); err != nil {
			return model.OrderResponse{}, err
		}
	}
	for _, a := range allocs {
		if _, err := tx.Exec(`INSERT INTO order_allocations (order_id, location_id, x, y, sku, qty, priority)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, orderID, a.LocationID, a.X, a.Y, a.SKU, a.Qty, a.Priority); err != nil {
			return model.OrderResponse{}, err
		}
	}
	for _, d := range deductions {
		res, err := tx.Exec(`UPDATE inventory SET quantity = quantity - ? WHERE id = ? AND quantity >= ?`,
			d.take, d.inventoryID, d.take)
		if err != nil {
			return model.OrderResponse{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return model.OrderResponse{}, fmt.Errorf("库存并发变更，请重试")
		}
	}
	if err := tx.Commit(); err != nil {
		return model.OrderResponse{}, err
	}

	return model.OrderResponse{
		ID:          orderID,
		Status:      status,
		Allocations: allocs,
		Shortages:   shortages,
		Route:       rt,
		CreatedAt:   time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// OrderSummary 是历史订单列表项的摘要信息。
type OrderSummary struct {
	ID            int64  `json:"id"`
	Status        string `json:"status"`
	TotalDistance int    `json:"totalDistance"`
	ItemCount     int    `json:"itemCount"`
	CreatedAt     string `json:"createdAt"`
}

// ListOrders 返回最近 50 笔订单的摘要。
func ListOrders(db *sql.DB) ([]OrderSummary, error) {
	rows, err := db.Query(`SELECT o.id, o.status, o.total_distance,
		(SELECT COUNT(*) FROM order_items oi WHERE oi.order_id = o.id),
		strftime('%Y-%m-%d %H:%M:%S', o.created_at)
		FROM orders o ORDER BY o.id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []OrderSummary{}
	for rows.Next() {
		var s OrderSummary
		if err := rows.Scan(&s.ID, &s.Status, &s.TotalDistance, &s.ItemCount, &s.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetOrder 查询指定订单的分配明细与动线。
func GetOrder(db *sql.DB, id int64) (model.OrderResponse, error) {
	var (
		resp      model.OrderResponse
		routeJSON sql.NullString
		createdAt string
	)
	err := db.QueryRow(`SELECT id, status, total_distance, route_json, strftime('%Y-%m-%d %H:%M:%S', created_at)
		FROM orders WHERE id=?`, id).
		Scan(&resp.ID, &resp.Status, &resp.Route.Distance, &routeJSON, &createdAt)
	if err != nil {
		return resp, err
	}
	resp.CreatedAt = createdAt
	if routeJSON.Valid && routeJSON.String != "" {
		_ = json.Unmarshal([]byte(routeJSON.String), &resp.Route)
	}

	rows, err := db.Query(`SELECT location_id, x, y, sku, qty, priority FROM order_allocations WHERE order_id=? ORDER BY id`, id)
	if err != nil {
		return resp, err
	}
	resp.Allocations = []model.Allocation{}
	allocatedBySKU := map[string]int{}
	for rows.Next() {
		var a model.Allocation
		if err := rows.Scan(&a.LocationID, &a.X, &a.Y, &a.SKU, &a.Qty, &a.Priority); err != nil {
			rows.Close()
			return resp, err
		}
		resp.Allocations = append(resp.Allocations, a)
		allocatedBySKU[a.SKU] += a.Qty
	}
	rows.Close()

	itemRows, err := db.Query(`SELECT sku, requested_qty FROM order_items WHERE order_id=? ORDER BY id`, id)
	if err != nil {
		return resp, err
	}
	resp.Shortages = []model.Shortage{}
	for itemRows.Next() {
		var sku string
		var requested int
		if err := itemRows.Scan(&sku, &requested); err != nil {
			itemRows.Close()
			return resp, err
		}
		if allocatedBySKU[sku] < requested {
			resp.Shortages = append(resp.Shortages, model.Shortage{SKU: sku, Requested: requested, Allocated: allocatedBySKU[sku]})
		}
	}
	itemRows.Close()

	return resp, nil
}
