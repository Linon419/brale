# 多指标背离评分系统设计

## 概述

用新的多指标背离评分系统替换现有的 `macd.divergence`，支持 10 个指标的背离检测、加权评分、动态阈值和自适应权重学习。

## 架构

```
┌─────────────────────────────────────────────────────┐
│              多指标背离评分系统                        │
├─────────────────────────────────────────────────────┤
│  输入：divergence_multi.go 的 MultiDivSignal[]       │
│                                                     │
│  ┌──────────────┐    ┌──────────────┐              │
│  │ 动量组 (6个)  │    │ 成交量组 (4个) │              │
│  │ 基础分=1     │    │ 基础分=2      │              │
│  │ 范围[0.5,2]  │    │ 范围[1,4]     │              │
│  └──────┬───────┘    └──────┬───────┘              │
│         │                   │                       │
│         └─────────┬─────────┘                       │
│                   ▼                                 │
│         ┌─────────────────┐                        │
│         │ 计算加权总分     │                        │
│         └────────┬────────┘                        │
│                  ▼                                 │
│         ┌─────────────────┐                        │
│         │ 动态阈值 (40%)   │                        │
│         └────────┬────────┘                        │
│                  ▼                                 │
│         ┌─────────────────┐                        │
│         │ 输出：是否有效   │                        │
│         └─────────────────┘                        │
└─────────────────────────────────────────────────────┘
```

## 指标分组

| 分组 | 指标 | 基础权重 | 权重范围 |
|------|------|----------|----------|
| 动量组 | MACD, MACD_Hist, RSI, Stoch, CCI, Momentum | 1 | [0.5, 2] |
| 成交量组 | OBV, VWMACD, CMF, MFI | 2 | [1, 4] |

**满分：** 6×1 + 4×2 = 14 分

## 自适应权重系统

### 数据记录结构

```go
type DivergenceRecord struct {
    Timestamp   time.Time
    Indicator   string   // 指标名
    Type        string   // positive_regular/negative_regular/...
    Symbol      string   // 交易对
    Timeframe   string   // 15m/1h/4h

    // 动态验证结果
    DynamicSuccess  bool     // ATR 验证是否成功
    PriceMove       float64  // 后续价格变动 %

    // 实际交易结果（可选）
    TradeTriggered  bool
    TradeProfit     float64
}
```

### 权重计算公式

```
样本数 N = 该指标近期记录数（半衰期加权）

if N < 20:
    权重 = 初始权重  // 样本不足，用默认值
else:
    动态验证准确率 = Σ(成功×衰减权重) / Σ(衰减权重)
    实际交易胜率 = Σ(盈利×衰减权重) / Σ(衰减权重)

    综合准确率 = 0.3×动态验证 + 0.7×实际交易
    // 如果交易记录不足，自动调整比例

    权重 = 初始权重 × (0.5 + 综合准确率)
    权重 = clamp(权重, 最小值, 最大值)
```

### 时间衰减

```
衰减权重 = 0.5 ^ (天数差 / 30)
// 30天前 = 0.5，60天前 = 0.25，90天前 = 0.125
```

## 动态验证逻辑

### 验证配置

```go
type ValidationConfig struct {
    ATRMultiplier  float64  // 反转幅度 = ATR × 1.5
    WindowBars     map[string]int  // 验证窗口
}

// 验证窗口（根据周期）
WindowBars = {
    "15m": 20,  // 5小时内
    "1h":  12,  // 12小时内
    "4h":  8,   // 32小时内
}
```

### 验证流程

1. 背离信号出现，记录当前价格 P0 和 ATR
2. 等待 N 根 K 线（根据周期）
3. 判定成功条件：
   - positive 背离（看涨）：最高价 >= P0 × (1 + ATR×1.5/P0)
   - negative 背离（看跌）：最低价 <= P0 × (1 - ATR×1.5/P0)
4. 记录结果到 DivergenceRecord

## 动态阈值与信号判定

### 阈值计算

```go
func CalculateThreshold(signals []MultiDivSignal, weights map[string]float64) float64 {
    // 1. 计算当前活跃指标的满分
    maxScore := 0.0
    for _, sig := range signals {
        maxScore += weights[sig.Indicator]
    }

    // 2. 阈值 = 满分 × 40%
    return maxScore * 0.4
}
```

### 信号判定流程

```
输入：MultiDivSignal[] 来自 divergence_multi.go

1. 按方向分组
   - 看涨组：positive_regular + positive_hidden
   - 看跌组：negative_regular + negative_hidden

2. 分别计算加权得分
   看涨得分 = Σ(看涨信号的指标权重)
   看跌得分 = Σ(看跌信号的指标权重)

3. 计算各自阈值并判定
   看涨有效 = 看涨得分 >= 看涨阈值
   看跌有效 = 看跌得分 >= 看跌阈值

4. 输出
   - "up"   : 仅看涨有效
   - "down" : 仅看跌有效
   - "conflict" : 两边都有效（谨慎）
   - "none" : 都不有效
```

## 整合到 hedge_trend.txt

### 修改前

```
3) 震荡交易（反转）：
   - `wt_mfi_hybrid` 进入超买/超卖区域才考虑反转。
   - 出现 `macd.divergence` 方向确认（up/down），否则 hold。
```

### 修改后

```
3) 震荡交易（反转）：
   - `wt_mfi_hybrid` 进入超买/超卖区域才考虑反转。
   - `multi_div` 背离评分确认：
     - "up": 看涨背离有效，考虑做多
     - "down": 看跌背离有效，考虑做空
     - "conflict": 多空冲突，hold
     - "none": 无有效背离，hold
   - 评分规则：动量组(MACD/RSI/Stoch/CCI/Mom/MACD_Hist)×自适应权重 +
              成交量组(OBV/VWMACD/CMF/MFI)×自适应权重
   - 阈值 = 活跃指标满分 × 40%
```

### 数据模板新增

```
{{ .MultiDivData }}
// 输出示例：
// multi_div: {
//   direction: "up",
//   score: 5.3,
//   threshold: 4.2,
//   signals: [{indicator: "macd", type: "positive_regular"}, ...]
// }
```

## 实现清单

1. [ ] 创建 `DivergenceRecord` 结构和存储
2. [ ] 实现权重计算服务（含衰减逻辑）
3. [ ] 实现动态验证追踪器
4. [ ] 创建评分计算函数
5. [ ] 整合到 analysis context
6. [ ] 修改 hedge_trend.txt prompt
7. [ ] 添加 `{{ .MultiDivData }}` 模板变量
