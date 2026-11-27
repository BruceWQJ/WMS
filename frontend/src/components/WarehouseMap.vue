<script setup>
// WarehouseMap 仓库网格地图：SVG 绘制库位、单行道箭头、出入口、拣料点与动线路径。
import { computed } from 'vue'

const props = defineProps({
  warehouse: { type: Object, required: true }, // 仓库网格与出入口配置
  locations: { type: Array, default: () => [] }, // 库位列表（含库存与方向）
  route: { type: Object, default: null }, // 当前订单动线（路径、拣料点、可达性）
  activeIndex: { type: Number, default: -1 }, // 动线播放当前下标
  selected: { type: Object, default: null } // 当前选中的格点
})
const emit = defineEmits(['select-cell'])

// 网格步距、格点尺寸与画布边距
const pitch = 29
const rectSize = 27
const pad = 22

const width = computed(() => props.warehouse?.width || 20)
const height = computed(() => props.warehouse?.height || 20)
// SVG 画布尺寸（viewBox）
const vbWidth = computed(() => width.value * pitch + pad * 2)
const vbHeight = computed(() => height.value * pitch + pad * 2)

// 坐标 -> 库位 的查找表，便于按格点取属性
const locMap = computed(() => {
  const map = new Map()
  for (const loc of props.locations || []) map.set(loc.x + ',' + loc.y, loc)
  return map
})

// 生成网格内全部格点（含对应库位，可能为空）
const cells = computed(() => {
  const result = []
  for (let y = 0; y < height.value; y++) {
    for (let x = 0; x < width.value; x++) {
      result.push({ x, y, loc: locMap.value.get(x + ',' + y) })
    }
  }
  return result
})

// 坐标换算为 SVG 像素中心
const cx = (x) => pad + x * pitch + pitch / 2
const cy = (y) => pad + y * pitch + pitch / 2

// 通行方向 -> 箭头字符
const arrowOf = (dir) => ({
  NORTH_TO_SOUTH: '↓',
  SOUTH_TO_NORTH: '↑',
  EAST_TO_WEST: '←',
  WEST_TO_EAST: '→'
}[dir] || '')

// 库位底色：高架蓝、地堆绿，空格灰色
const fillOf = (loc) => {
  if (!loc) return '#f8fafc'
  return loc.storageType === 'RACK' ? '#dbeafe' : '#dcfce7'
}
// 库位边框色，与底色对应
const strokeOf = (loc) => {
  if (!loc) return '#e2e8f0'
  return loc.storageType === 'RACK' ? '#3b82f6' : '#22c55e'
}
// 判断格点是否为当前选中格
const isSelected = (cell) => !!props.selected && props.selected.x === cell.x && props.selected.y === cell.y

// 动线路径折线坐标串
const routePoints = computed(() => (props.route?.path || []).map((p) => `${cx(p.x)},${cy(p.y)}`).join(' '))
// 播放高亮的当前位置
const activePoint = computed(() => props.route?.path?.[props.activeIndex] || null)
// 带访问序号的拣料点
const stopPoints = computed(() => (props.route?.stops || []).map((p, i) => ({ ...p, index: i + 1 })))
const startPoint = computed(() => ({ x: props.warehouse?.startX ?? 0, y: props.warehouse?.startY ?? 0 }))
const endPoint = computed(() => ({ x: props.warehouse?.endX ?? 0, y: props.warehouse?.endY ?? 0 }))
</script>

<template>
  <div class="map-scroll">
    <svg :viewBox="`0 0 ${vbWidth} ${vbHeight}`" :width="vbWidth" :height="vbHeight" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <marker id="routeArrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto">
          <path d="M 0 0 L 10 5 L 0 10 z" fill="#f97316" />
        </marker>
        <filter id="glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="2" result="blur" />
          <feMerge>
            <feMergeNode in="blur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>

      <rect v-for="cell in cells" :key="cell.x + '-' + cell.y"
        :x="pad + cell.x * pitch + 1" :y="pad + cell.y * pitch + 1"
        :width="rectSize" :height="rectSize" rx="3"
        :fill="fillOf(cell.loc)" :stroke="strokeOf(cell.loc)"
        :stroke-width="isSelected(cell) ? 2.5 : 1"
        :stroke-dasharray="isSelected(cell) ? '3 2' : ''"
        class="grid-cell"
        @click="emit('select-cell', { x: cell.x, y: cell.y })" />

      <template v-for="cell in cells" :key="'info-' + cell.x + '-' + cell.y">
        <text v-if="cell.loc && cell.loc.direction && cell.loc.direction !== 'BIDIRECTIONAL'"
          :x="cx(cell.x)" :y="cy(cell.y) + 5" text-anchor="middle" font-size="14" fill="#1e3a8a" font-weight="bold" pointer-events="none">
          {{ arrowOf(cell.loc.direction) }}
        </text>
        <text v-if="cell.loc && cell.loc.inventory && Object.keys(cell.loc.inventory).length"
          :x="cx(cell.x) + 9" :y="cy(cell.y) + 9" text-anchor="middle" font-size="8" fill="#475569" pointer-events="none">
          {{ Object.values(cell.loc.inventory).reduce((a, b) => a + b, 0) }}
        </text>
      </template>

      <g pointer-events="none">
        <rect :x="pad + startPoint.x * pitch + 1" :y="pad + startPoint.y * pitch + 1" :width="rectSize" :height="rectSize" rx="3" fill="#fbbf24" stroke="#b45309" stroke-width="1.5" opacity="0.9" />
        <text :x="cx(startPoint.x)" :y="cy(startPoint.y) + 4" text-anchor="middle" font-size="10" font-weight="bold" fill="#78350f">S</text>
        <template v-if="endPoint.x !== startPoint.x || endPoint.y !== startPoint.y">
          <rect :x="pad + endPoint.x * pitch + 1" :y="pad + endPoint.y * pitch + 1" :width="rectSize" :height="rectSize" rx="3" fill="#a78bfa" stroke="#6d28d9" stroke-width="1.5" opacity="0.9" />
          <text :x="cx(endPoint.x)" :y="cy(endPoint.y) + 4" text-anchor="middle" font-size="10" font-weight="bold" fill="#4c1d95">E</text>
        </template>
        <template v-else>
          <circle :cx="cx(endPoint.x) + 9" :cy="cy(endPoint.y) - 9" r="7" fill="#a78bfa" stroke="#6d28d9" stroke-width="1" />
          <text :x="cx(endPoint.x) + 9" :y="cy(endPoint.y) - 6" text-anchor="middle" font-size="8" font-weight="bold" fill="#4c1d95">E</text>
        </template>
      </g>

      <polyline v-if="routePoints" :points="routePoints" fill="none" stroke="#f97316" stroke-width="2.5"
        stroke-linejoin="round" stroke-linecap="round" marker-mid="url(#routeArrow)" opacity="0.9" pointer-events="none" />

      <g v-for="stop in stopPoints" :key="'stop-' + stop.index" pointer-events="none">
        <circle :cx="cx(stop.x)" :cy="cy(stop.y)" r="9" fill="#fff" stroke="#f97316" stroke-width="2" />
        <text :x="cx(stop.x)" :y="cy(stop.y) + 3.5" text-anchor="middle" font-size="10" fill="#c2410c" font-weight="bold">{{ stop.index }}</text>
      </g>

      <circle v-if="activePoint" :cx="cx(activePoint.x)" :cy="cy(activePoint.y)" r="7" fill="#f97316" opacity="0.85" filter="url(#glow)" pointer-events="none" />
    </svg>
  </div>
</template>

<style scoped>
.grid-cell { cursor: pointer; transition: fill .12s; }
.grid-cell:hover { fill: #fed7aa; }
</style>
