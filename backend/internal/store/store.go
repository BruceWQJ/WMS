// store 负责 SQLite 连接、表结构迁移，以及仓库 / 库位 / 库存的数据访问。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"

	"wms/backend/internal/model"
)

// dbtx 抽象 *sql.DB 与 *sql.Tx，便于在事务中复用查询逻辑。
type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// OpenDB 打开（必要时创建）SQLite 数据库并执行表结构迁移。
func OpenDB(sqlitePath string) (*sql.DB, error) {
	abs, err := filepath.Abs(sqlitePath)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("迁移表结构失败: %w", err)
	}
	return db, nil
}

// migrate 建表（不存在则创建），完成数据库表结构初始化。
func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS warehouse (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT 'Default Warehouse',
			width INTEGER NOT NULL DEFAULT 20,
			height INTEGER NOT NULL DEFAULT 20,
			start_x INTEGER NOT NULL DEFAULT 0,
			start_y INTEGER NOT NULL DEFAULT 0,
			end_x INTEGER NOT NULL DEFAULT 0,
			end_y INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS location (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			x INTEGER NOT NULL,
			y INTEGER NOT NULL,
			storage_type TEXT NOT NULL DEFAULT 'FLOOR',
			aisle_id TEXT NULL,
			direction TEXT NOT NULL DEFAULT 'BIDIRECTIONAL',
			picking_priority INTEGER NOT NULL DEFAULT 0,
			UNIQUE (x, y)
		)`,
		`CREATE TABLE IF NOT EXISTS inventory (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			location_id INTEGER NOT NULL,
			sku TEXT NOT NULL,
			quantity INTEGER NOT NULL DEFAULT 0,
			UNIQUE (location_id, sku),
			FOREIGN KEY (location_id) REFERENCES location(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status TEXT NOT NULL DEFAULT 'ALLOCATED',
			total_distance INTEGER NOT NULL DEFAULT 0,
			route_json TEXT NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S','now','localtime'))
		)`,
		`CREATE TABLE IF NOT EXISTS order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id INTEGER NOT NULL,
			sku TEXT NOT NULL,
			requested_qty INTEGER NOT NULL,
			FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS order_allocations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id INTEGER NOT NULL,
			location_id INTEGER NOT NULL,
			x INTEGER NOT NULL,
			y INTEGER NOT NULL,
			sku TEXT NOT NULL,
			qty INTEGER NOT NULL,
			priority INTEGER NOT NULL,
			FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// GetWarehouse 读取当前仓库配置；无记录时写入一条默认仓库。
func GetWarehouse(db *sql.DB) (model.Warehouse, error) {
	var w model.Warehouse
	err := db.QueryRow(`SELECT id, name, width, height, start_x, start_y, end_x, end_y FROM warehouse ORDER BY id LIMIT 1`).
		Scan(&w.ID, &w.Name, &w.Width, &w.Height, &w.StartX, &w.StartY, &w.EndX, &w.EndY)
	if errors.Is(err, sql.ErrNoRows) {
		res, insErr := db.Exec(`INSERT INTO warehouse (name, width, height) VALUES ('默认仓库', 20, 20)`)
		if insErr != nil {
			return w, insErr
		}
		id, _ := res.LastInsertId()
		return model.Warehouse{ID: id, Name: "默认仓库", Width: 20, Height: 20}, nil
	}
	return w, err
}

// validateWarehouse 校验仓库尺寸与起终点坐标是否合法。
func validateWarehouse(w model.Warehouse) error {
	if w.Width <= 0 || w.Height <= 0 {
		return errors.New("仓库宽高必须大于 0")
	}
	if w.Width > 60 || w.Height > 60 {
		return errors.New("仓库尺寸上限为 60x60")
	}
	for _, p := range []model.Point{{X: w.StartX, Y: w.StartY}, {X: w.EndX, Y: w.EndY}} {
		if p.X < 0 || p.Y < 0 || p.X >= w.Width || p.Y >= w.Height {
			return fmt.Errorf("起终点 (%d, %d) 超出仓库范围", p.X, p.Y)
		}
	}
	return nil
}

// CreateWarehouse 新建仓库；仓库已存在时返回错误。
func CreateWarehouse(db *sql.DB, w model.Warehouse) (model.Warehouse, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse`).Scan(&count); err != nil {
		return w, err
	}
	if count > 0 {
		return w, errors.New("仓库已存在，请使用 PUT 更新")
	}
	if err := validateWarehouse(w); err != nil {
		return w, err
	}
	res, err := db.Exec(`INSERT INTO warehouse (name, width, height, start_x, start_y, end_x, end_y)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, w.Name, w.Width, w.Height, w.StartX, w.StartY, w.EndX, w.EndY)
	if err != nil {
		return w, err
	}
	w.ID, _ = res.LastInsertId()
	return GetWarehouse(db)
}

// ResetWarehouse 将仓库恢复为默认 20×20 配置。
func ResetWarehouse(db *sql.DB) (model.Warehouse, error) {
	wh, err := GetWarehouse(db)
	if err != nil {
		return wh, err
	}
	_, err = db.Exec(`UPDATE warehouse SET name=?, width=20, height=20, start_x=0, start_y=0, end_x=0, end_y=0 WHERE id=?`,
		"默认仓库", wh.ID)
	if err != nil {
		return wh, err
	}
	return GetWarehouse(db)
}

// UpdateWarehouse 更新仓库配置。
func UpdateWarehouse(db *sql.DB, w model.Warehouse) (model.Warehouse, error) {
	if err := validateWarehouse(w); err != nil {
		return w, err
	}
	if w.ID == 0 {
		existing, err := GetWarehouse(db)
		if err != nil {
			return w, err
		}
		w.ID = existing.ID
	}
	_, err := db.Exec(`UPDATE warehouse SET name=?, width=?, height=?, start_x=?, start_y=?, end_x=?, end_y=? WHERE id=?`,
		w.Name, w.Width, w.Height, w.StartX, w.StartY, w.EndX, w.EndY, w.ID)
	if err != nil {
		return w, err
	}
	return GetWarehouse(db)
}

// ListLocations 返回全部库位及其库存明细。
func ListLocations(q dbtx) ([]model.Location, error) {
	rows, err := q.Query(`SELECT id, x, y, storage_type, COALESCE(aisle_id,''), direction, picking_priority FROM location ORDER BY y, x`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	locs := []model.Location{}
	index := map[int64]int{}
	for rows.Next() {
		var l model.Location
		if err := rows.Scan(&l.ID, &l.X, &l.Y, &l.StorageType, &l.AisleID, &l.Direction, &l.PickingPriority); err != nil {
			return nil, err
		}
		l.Inventory = map[string]int{}
		index[l.ID] = len(locs)
		locs = append(locs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	invRows, err := q.Query(`SELECT location_id, sku, quantity FROM inventory WHERE quantity > 0 ORDER BY sku`)
	if err != nil {
		return nil, err
	}
	defer invRows.Close()
	for invRows.Next() {
		var locID int64
		var sku string
		var qty int
		if err := invRows.Scan(&locID, &sku, &qty); err != nil {
			return nil, err
		}
		if i, ok := index[locID]; ok {
			locs[i].Inventory[sku] = qty
		}
	}
	return locs, invRows.Err()
}

// GetLocation 按 ID 查询单个库位及其库存。
func GetLocation(q dbtx, id int64) (model.Location, error) {
	var l model.Location
	err := q.QueryRow(`SELECT id, x, y, storage_type, COALESCE(aisle_id,''), direction, picking_priority FROM location WHERE id=?`, id).
		Scan(&l.ID, &l.X, &l.Y, &l.StorageType, &l.AisleID, &l.Direction, &l.PickingPriority)
	if err != nil {
		return l, err
	}
	l.Inventory = map[string]int{}
	rows, err := q.Query(`SELECT sku, quantity FROM inventory WHERE location_id=? AND quantity > 0 ORDER BY sku`, id)
	if err != nil {
		return l, err
	}
	defer rows.Close()
	for rows.Next() {
		var sku string
		var qty int
		if err := rows.Scan(&sku, &qty); err != nil {
			return l, err
		}
		l.Inventory[sku] = qty
	}
	return l, rows.Err()
}

// ListLocationAttrs 返回全部库位的属性（不含库存），供动线算法使用。
func ListLocationAttrs(q dbtx) ([]model.Location, error) {
	rows, err := q.Query(`SELECT id, x, y, storage_type, COALESCE(aisle_id,''), direction, picking_priority FROM location`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locs := []model.Location{}
	for rows.Next() {
		var l model.Location
		if err := rows.Scan(&l.ID, &l.X, &l.Y, &l.StorageType, &l.AisleID, &l.Direction, &l.PickingPriority); err != nil {
			return nil, err
		}
		locs = append(locs, l)
	}
	return locs, rows.Err()
}

// replaceInventory 清空并重建某库位的库存记录。
func replaceInventory(tx dbtx, locationID int64, inv map[string]int) error {
	if _, err := tx.Exec(`DELETE FROM inventory WHERE location_id=?`, locationID); err != nil {
		return err
	}
	skus := make([]string, 0, len(inv))
	for sku := range inv {
		skus = append(skus, sku)
	}
	sort.Strings(skus)
	for _, sku := range skus {
		qty := inv[sku]
		if sku == "" || qty <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO inventory (location_id, sku, quantity) VALUES (?, ?, ?)`, locationID, sku, qty); err != nil {
			return err
		}
	}
	return nil
}

// SaveLocation 校验并新建或更新库位，同坐标冲突时覆盖更新。
func SaveLocation(db *sql.DB, l model.Location) (model.Location, error) {
	if l.StorageType != "FLOOR" && l.StorageType != "RACK" {
		return l, errors.New("storage_type 只能为 FLOOR 或 RACK")
	}
	if l.Direction == "" {
		if l.StorageType == "RACK" {
			l.Direction = "NORTH_TO_SOUTH"
		} else {
			l.Direction = "BIDIRECTIONAL"
		}
	}
	switch l.Direction {
	case "BIDIRECTIONAL", "NORTH_TO_SOUTH", "SOUTH_TO_NORTH", "EAST_TO_WEST", "WEST_TO_EAST":
	default:
		return l, errors.New("非法的通行方向")
	}
	if l.StorageType == "RACK" && l.Direction != "NORTH_TO_SOUTH" && l.Direction != "SOUTH_TO_NORTH" {
		return l, errors.New("高架区必须配置为单行道，方向只能是 NORTH_TO_SOUTH 或 SOUTH_TO_NORTH")
	}
	if l.X < 0 || l.Y < 0 {
		return l, errors.New("坐标不能为负")
	}

	tx, err := db.Begin()
	if err != nil {
		return l, err
	}
	defer tx.Rollback()

	var locID int64
	if l.ID > 0 {
		res, err := tx.Exec(`UPDATE location SET x=?, y=?, storage_type=?, aisle_id=?, direction=?, picking_priority=? WHERE id=?`,
			l.X, l.Y, l.StorageType, nullIfEmpty(l.AisleID), l.Direction, l.PickingPriority, l.ID)
		if err != nil {
			return l, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return l, sql.ErrNoRows
		}
		locID = l.ID
	} else {
		res, err := tx.Exec(`INSERT INTO location (x, y, storage_type, aisle_id, direction, picking_priority)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(x, y) DO UPDATE SET
			storage_type=excluded.storage_type, aisle_id=excluded.aisle_id,
			direction=excluded.direction, picking_priority=excluded.picking_priority`,
			l.X, l.Y, l.StorageType, nullIfEmpty(l.AisleID), l.Direction, l.PickingPriority)
		if err != nil {
			return l, err
		}
		locID, _ = res.LastInsertId()
		if locID == 0 {
			if err := tx.QueryRow(`SELECT id FROM location WHERE x=? AND y=?`, l.X, l.Y).Scan(&locID); err != nil {
				return l, err
			}
		}
	}

	if l.Inventory != nil {
		if err := replaceInventory(tx, locID, l.Inventory); err != nil {
			return l, err
		}
	}
	if err := tx.Commit(); err != nil {
		return l, err
	}

	l.ID = locID
	if l.Inventory == nil {
		l.Inventory = map[string]int{}
	}
	return l, nil
}

// DeleteLocation 按 ID 删除库位；记录不存在时返回 sql.ErrNoRows。
func DeleteLocation(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM location WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetInventory 写入或覆盖某库位单个 SKU 的库存数量。
func SetInventory(db *sql.DB, item model.InventoryItem) error {
	if item.SKU == "" {
		return errors.New("sku 不能为空")
	}
	if item.Quantity < 0 {
		return errors.New("数量不能为负")
	}
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM location WHERE id=?`, item.LocationID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("库位 #%d 不存在", item.LocationID)
	}
	_, err := db.Exec(`INSERT INTO inventory (location_id, sku, quantity) VALUES (?, ?, ?)
		ON CONFLICT(location_id, sku) DO UPDATE SET quantity=excluded.quantity`, item.LocationID, item.SKU, item.Quantity)
	return err
}

// ListInventory 返回全部库存明细。
func ListInventory(db *sql.DB) ([]model.InventoryItem, error) {
	rows, err := db.Query(`SELECT location_id, sku, quantity FROM inventory ORDER BY location_id, sku`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.InventoryItem{}
	for rows.Next() {
		var it model.InventoryItem
		if err := rows.Scan(&it.LocationID, &it.SKU, &it.Quantity); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// nullIfEmpty 空串转 NULL，非空原样返回。
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SeedWarehouse 清空既有业务数据并写入内置示例仓库。
func SeedWarehouse(db *sql.DB) (model.Warehouse, error) {
	tx, err := db.Begin()
	if err != nil {
		return model.Warehouse{}, err
	}
	defer tx.Rollback()

	for _, table := range []string{"order_allocations", "order_items", "orders", "inventory", "location", "warehouse"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return model.Warehouse{}, err
		}
	}

	const width, height = 20, 20
	if _, err := tx.Exec(`INSERT INTO warehouse (name, width, height, start_x, start_y, end_x, end_y) VALUES (?, ?, ?, 0, 0, 0, 0)`,
		"智慧仓库 A 区", width, height); err != nil {
		return model.Warehouse{}, err
	}

	rackCols := map[int]bool{4: true, 6: true, 8: true, 10: true, 12: true, 14: true}
	rackOrder := []int{4, 6, 8, 10, 12, 14}
	colDir := map[int]string{}
	for i, c := range rackOrder {
		if i%2 == 0 {
			colDir[c] = "NORTH_TO_SOUTH"
		} else {
			colDir[c] = "SOUTH_TO_NORTH"
		}
	}

	isCross := func(y int) bool { return y == 0 || y == height-1 || y%4 == 3 }
	skus := []string{"SKU_A", "SKU_B", "SKU_C", "SKU_D", "SKU_E", "SKU_F"}

	insertLoc := `INSERT INTO location (x, y, storage_type, aisle_id, direction, picking_priority) VALUES (?, ?, ?, ?, ?, ?)`
	insertInv := `INSERT INTO inventory (location_id, sku, quantity) VALUES (?, ?, ?)`

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var (
				storageType = "FLOOR"
				aisle       any
				direction   = "BIDIRECTIONAL"
				priority    = 10
				isRack      = rackCols[x] && !isCross(y)
			)
			if isRack {
				storageType = "RACK"
				aisle = fmt.Sprintf("A%02d", x)
				direction = colDir[x]
				priority = 5
			} else {
				floorStore := (x == 1 || x == 2 || x == 16 || x == 17) && !isCross(y)
				if !floorStore && !(x <= 2 && y <= 2) {
					continue
				}
				if x <= 2 && y <= 2 {
					priority = 10
				}
			}

			r, err := tx.Exec(insertLoc, x, y, storageType, aisle, direction, priority)
			if err != nil {
				return model.Warehouse{}, err
			}
			locID, _ := r.LastInsertId()

			sku := skus[(x+y)%len(skus)]
			qty := 20 + ((x*7+y*3)%5)*10
			if _, err := tx.Exec(insertInv, locID, sku, qty); err != nil {
				return model.Warehouse{}, err
			}
			if isRack && y%2 == 0 {
				alt := skus[(x+y+1)%len(skus)]
				if _, err := tx.Exec(insertInv, locID, alt, 10+((x+y)%4)*5); err != nil {
					return model.Warehouse{}, err
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return model.Warehouse{}, err
	}
	return GetWarehouse(db)
}
