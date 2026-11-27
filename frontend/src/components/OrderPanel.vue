<script setup>
// OrderPanel 订单面板：录入 SKU 与数量、提交拣料单、展示扣减明细，并控制动线播放与历史回看。
import { onMounted, reactive, ref, watch } from 'vue'
import { api } from '../api'

const props = defineProps({
  skus: { type: Array, default: () => [] }, // 可选 SKU 列表
  result: { type: Object, default: null }, // 当前订单结果
  activeIndex: { type: Number, default: -1 }, // 动线当前下标
  pathLength: { type: Number, default: 0 }, // 动线路径长度
  playing: { type: Boolean, default: false }, // 是否播放中
  speed: { type: Number, default: 300 } // 播放步进间隔
})
const emit = defineEmits(['created', 'toggle-play', 'reset', 'step', 'update:speed'])

// 订单明细行、错误提示、提交中状态、历史订单
const lines = reactive([])
const error = ref('')
const busy = ref(false)
const history = ref([])

// SKU 列表就绪后，默认填入一行首项 SKU
watch(() => props.skus, (list) => {
  if (lines.length === 0 && list.length) lines.push({ sku: list[0], qty: 10 })
}, { immediate: true })

// 新增一行明细
function addLine() {
  lines.push({ sku: props.skus[0] || '', qty: 10 })
}

// 删除指定明细行
function removeLine(index) {
  lines.splice(index, 1)
}

// 快捷勾选 / 取消某个 SKU
function toggleSku(sku) {
  const index = lines.findIndex((line) => line.sku === sku)
  if (index >= 0) {
    lines.splice(index, 1)
  } else {
    lines.push({ sku, qty: 10 })
  }
}

// 校验明细后提交拣料单，成功后刷新历史订单
async function submit() {
  error.value = ''
  if (lines.length === 0) {
    error.value = '请先勾选或输入 SKU 与数量'
    return
  }
  const items = []
  for (const line of lines) {
    const sku = String(line.sku || '').trim()
    const qty = Number(line.qty)
    if (!sku || !Number.isInteger(qty) || qty <= 0) {
      error.value = '请填写合法的 SKU 与正整数数量'
      return
    }
    items.push({ sku, qty })
  }
  busy.value = true
  try {
    const result = await api.createOrder(items)
    emit('created', result)
    await loadHistory()
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

// 拉取历史订单列表
async function loadHistory() {
  try {
    history.value = await api.listOrders()
  } catch {
    history.value = []
  }
}

// 回看指定历史订单的明细与动线
async function openOrder(id) {
  try {
    const order = await api.getOrder(id)
    emit('created', order)
  } catch (e) {
    error.value = e.message
  }
}

onMounted(loadHistory)
</script>

<template>
  <div class="section panel">
    <h2>交互式订单 <span class="tag">生成拣料单</span></h2>

    <datalist id="orderSkuOptions">
      <option v-for="sku in skus" :key="sku" :value="sku" />
    </datalist>

    <div v-if="skus.length" class="toolbar" style="margin-bottom: 8px;">
      <span class="hint">快捷勾选：</span>
      <button v-for="sku in skus" :key="sku" :class="lines.some((line) => line.sku === sku) ? 'primary' : ''"
        @click="toggleSku(sku)">{{ sku }}</button>
    </div>

    <div v-for="(line, index) in lines" :key="index" class="order-line">
      <input v-model="line.sku" list="orderSkuOptions" class="sku" placeholder="SKU_A" />
      <input v-model.number="line.qty" type="number" min="1" class="qty" />
      <button @click="removeLine(index)">×</button>
    </div>

    <div class="toolbar" style="margin-top: 8px;">
      <button @click="addLine">+ 添加明细</button>
      <span class="grow"></span>
      <button class="primary" :disabled="busy" @click="submit">生成拣料单</button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>

    <div v-if="result" style="margin-top: 12px; border-top: 1px solid #e2e8f0; padding-top: 10px;">
      <div class="toolbar" style="margin-bottom: 8px;">
        <span class="chip" :class="result.status === 'ALLOCATED' ? 'green' : 'red'">
          订单 #{{ result.id }} · {{ result.status }}
        </span>
        <span class="chip">总路径 {{ result.route?.distance ?? 0 }} 步</span>
        <span class="chip">拣料点 {{ result.route?.stops?.length || 0 }}</span>
      </div>

      <p v-if="result.route && !result.route.reachable" class="error">{{ result.route.message }}</p>

      <p v-if="result.shortages?.length" class="error">
        库存不足：
        <span v-for="s in result.shortages" :key="s.sku">{{ s.sku }} 需 {{ s.requested }} / 可分配 {{ s.allocated }}；</span>
      </p>

      <table v-if="result.allocations?.length" style="margin-top: 4px;">
        <thead>
          <tr><th>库位</th><th>SKU</th><th>扣减</th><th>优先级</th></tr>
        </thead>
        <tbody>
          <tr v-for="(a, index) in result.allocations" :key="index">
            <td>({{ a.x }}, {{ a.y }})</td>
            <td>{{ a.sku }}</td>
            <td>{{ a.qty }}</td>
            <td>{{ a.priority }}</td>
          </tr>
        </tbody>
      </table>
      <p v-else class="empty">本次订单没有可扣减库存</p>

      <div v-if="result.route?.reachable && pathLength" class="toolbar" style="margin-top: 10px;">
        <button @click="emit('toggle-play')">{{ playing ? '暂停' : '播放动线' }}</button>
        <button @click="emit('step', -1)" :disabled="activeIndex <= 0">上一步</button>
        <button @click="emit('step', 1)" :disabled="activeIndex >= pathLength - 1">下一步</button>
        <button @click="emit('reset')">重置</button>
        <span class="grow"></span>
        <span class="hint">{{ pathLength ? (activeIndex + 1) + ' / ' + pathLength : '0 / 0' }}</span>
        <select :value="speed" @change="emit('update:speed', Number($event.target.value))">
          <option :value="600">慢速</option>
          <option :value="300">正常</option>
          <option :value="120">快速</option>
        </select>
      </div>
    </div>
  </div>

  <div class="section panel">
    <h2>历史订单 <span class="tag">点击回看动线</span></h2>
    <div v-if="history.length" class="toolbar" style="flex-wrap: wrap;">
      <button v-for="o in history" :key="o.id" @click="openOrder(o.id)" :class="result?.id === o.id ? 'primary' : ''">
        #{{ o.id }} · {{ o.itemCount }} 项 · {{ o.totalDistance }} 步
      </button>
    </div>
    <p v-else class="empty">暂无历史订单</p>
  </div>
</template>
