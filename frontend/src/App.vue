<script setup>
// App 根组件：统筹仓库/库位数据、订单结果与动线播放状态，组合地图、库位编辑与订单面板。
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api } from './api'
import WarehouseMap from './components/WarehouseMap.vue'
import CellEditor from './components/CellEditor.vue'
import OrderPanel from './components/OrderPanel.vue'

// 仓库配置、库位列表、当前选中格点、订单结果
const warehouse = ref({})
const locations = ref([])
const selected = ref(null)
const result = ref(null)
// 动线播放：当前路径下标、是否播放中、步进间隔（毫秒）
const activeIndex = ref(-1)
const playing = ref(false)
const speed = ref(300)
const error = ref('')
const busy = ref(false)

// 仓库配置表单（名称、宽高）
const whForm = reactive({ name: '', width: 20, height: 20 })

// 当前选中格点对应的库位对象
const selectedLocation = computed(() => {
  if (!selected.value) return null
  return locations.value.find((l) => l.x === selected.value.x && l.y === selected.value.y) || null
})

// 汇总全部库存中出现过的 SKU，供订单与库位编辑下拉选择
const skus = computed(() => {
  const set = new Set()
  for (const loc of locations.value) {
    for (const sku of Object.keys(loc.inventory || {})) set.add(sku)
  }
  return Array.from(set).sort()
})

// 当前动线路径长度
const pathLength = computed(() =>
  result.value?.route?.reachable ? result.value.route.path?.length || 0 : 0
)

// 仓库数据变化时同步到配置表单
watch(warehouse, (w) => {
  if (!w?.id) return
  whForm.name = w.name
  whForm.width = w.width
  whForm.height = w.height
})

// 拉取仓库配置与库位列表
async function loadData() {
  busy.value = true
  error.value = ''
  try {
    const [wh, locs] = await Promise.all([api.getWarehouse(), api.listLocations()])
    warehouse.value = wh
    locations.value = locs
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

// 保存仓库网格配置（出入口固定为 0,0）
async function saveWarehouse() {
  busy.value = true
  try {
    warehouse.value = await api.updateWarehouse({
      id: warehouse.value.id,
      name: whForm.name,
      width: Number(whForm.width),
      height: Number(whForm.height),
      startX: 0,
      startY: 0,
      endX: 0,
      endY: 0
    })
    reset()
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

// 清空并重新生成内置示例仓库
async function reseed() {
  if (!confirm('重新生成示例仓库将清空当前所有库位和订单，是否继续？')) return
  busy.value = true
  try {
    const data = await api.seed()
    warehouse.value = data.warehouse
    locations.value = data.locations
    selected.value = null
    result.value = null
    reset()
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

// 库位保存成功后刷新列表
function onSaved() {
  loadData()
}

// 库位删除后取消选中并刷新列表
function onDeleted() {
  selected.value = null
  loadData()
}

// 订单创建或回看后，记录结果并初始化动线播放位置
function onCreated(order) {
  stopTimer()
  result.value = order
  activeIndex.value = order.route?.reachable && order.route.path?.length ? 0 : -1
  loadData()
}

let timer = null

// 停止播放并清除定时器
function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  playing.value = false
}

// 按当前速度启动自动步进播放
function startTimer() {
  stopTimer()
  if (!pathLength.value) return
  playing.value = true
  timer = setInterval(() => {
    if (activeIndex.value >= pathLength.value - 1) {
      stopTimer()
      return
    }
    activeIndex.value++
  }, speed.value)
}

// 播放 / 暂停切换
function togglePlay() {
  if (!pathLength.value) return
  if (playing.value) {
    stopTimer()
    return
  }
  if (activeIndex.value >= pathLength.value - 1) activeIndex.value = 0
  startTimer()
}

// 重置到路径起点
function reset() {
  stopTimer()
  activeIndex.value = pathLength.value ? 0 : -1
}

// 手动单步前进 / 后退，delta 为步长（+1 或 -1）
function step(delta) {
  stopTimer()
  if (!pathLength.value) return
  activeIndex.value = Math.max(0, Math.min(pathLength.value - 1, activeIndex.value + delta))
}

// 调整播放速度，播放中则按新速度重启定时器
function changeSpeed(value) {
  speed.value = value
  if (playing.value) startTimer()
}

onMounted(loadData)
onUnmounted(stopTimer)
</script>

<template>
  <div class="app">
    <header class="topbar">
      <h1>WMS · 智慧仓库库位建模与拣料动线优化</h1>
      <span class="badge">Go + Vue 3 + SQLite</span>
      <span class="spacer"></span>
      <button :disabled="busy" @click="loadData">刷新数据</button>
      <button :disabled="busy" @click="reseed">重新生成示例</button>
    </header>

    <div class="content">
      <section class="panel map-panel">
        <div class="panel-title">
          仓库网格地图
          <span class="sub">{{ warehouse.width || 0 }} × {{ warehouse.height || 0 }} · 点击任意网格编辑库位</span>
        </div>
        <WarehouseMap
          :warehouse="warehouse"
          :locations="locations"
          :route="result?.route || null"
          :active-index="activeIndex"
          :selected="selected"
          @select-cell="selected = $event" />
        <div class="legend">
          <span><i style="background:#dcfce7;border-color:#22c55e"></i> 地堆区 FLOOR</span>
          <span><i style="background:#dbeafe;border-color:#3b82f6"></i> 高架区 RACK</span>
          <span><i style="background:#fbbf24;border-color:#b45309"></i> Pickstart (0,0)</span>
          <span><i style="background:#a78bfa;border-color:#6d28d9"></i> Pickend (0,0)</span>
          <span><i style="background:#fff;border-color:#f97316"></i> 拣料点（按访问顺序编号）</span>
          <span>单行道箭头：↑ ↓ ← →</span>
        </div>
      </section>

      <aside class="side-panel">
        <p v-if="error" class="error">{{ error }}</p>

        <div class="section panel">
          <h2>仓库配置 <span class="tag">网格与出入口</span></h2>
          <div class="field-row">
            <div class="field">
              <label>名称</label>
              <input v-model="whForm.name" />
            </div>
          </div>
          <div class="field-row">
            <div class="field">
              <label>宽度 X</label>
              <input v-model.number="whForm.width" type="number" min="2" max="60" />
            </div>
            <div class="field">
              <label>高度 Y</label>
              <input v-model.number="whForm.height" type="number" min="2" max="60" />
            </div>
          </div>
          <div class="toolbar">
            <button :disabled="busy" @click="saveWarehouse">保存配置</button>
            <span class="hint">出入口固定为 (0, 0)</span>
          </div>
        </div>

        <CellEditor
          v-if="selected"
          :x="selected.x"
          :y="selected.y"
          :location="selectedLocation"
          :skus="skus"
          @saved="onSaved"
          @deleted="onDeleted"
          @close="selected = null" />

        <OrderPanel
          :skus="skus"
          :result="result"
          :active-index="activeIndex"
          :path-length="pathLength"
          :playing="playing"
          :speed="speed"
          @created="onCreated"
          @toggle-play="togglePlay"
          @reset="reset"
          @step="step"
          @update:speed="changeSpeed" />
      </aside>
    </div>
  </div>
</template>
