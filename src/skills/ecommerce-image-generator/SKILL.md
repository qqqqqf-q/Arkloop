# 电商主图提示词生成器

通过结构化表单收集需求，生成每张图片的专业提示词，经用户确认后批量调用 `image_generate` 出图。

## 触发行为

用户明确说"帮我生成电商图"、"做几张主图"等，此时用户意图明确，**不要做任何额外操作**（如引导上传到电商系统、要求补齐商品信息等）。直接进入核心流程，用 ask_user 表单收集需求后生成图片。

## 核心流程

```
收集需求 → 生成提示词 → 用户确认修改 → 批量生图
```

## Step 1: 收集需求

使用 `ask_user` 工具（display_mode: "form"）一次性收集以下所有字段。按顺序排列，分两个表单展示。

### 第一个表单：产品与营销信息

| key | type | title | 说明 |
|-----|------|-------|------|
| `design_brief` | string | 设计简报 | 多行文本。产品核心卖点、USP、视觉方向、希望强调的卖点。maxLength: 300 |
| `promotion_info` | string | 促销信息 | 多行文本。促销活动详情：折扣力度、活动名称、优惠信息等。maxLength: 300 |
| `language` | string | 输出语言 | enum: ["zh", "en"], enumNames: ["中文", "English"] |

### 第二个表单：图像配置与模块选择

| key | type | title | 说明 |
|-----|------|-------|------|
| `version` | string | 主图版本（必选） | enum: ["practical", "cinematic"], enumNames: ["实用版 - 清晰还原，快速出图", "大片版 - 高级质感，品牌差异，转化率更高"]。required: true |
| `aspect_ratio` | string | 宽高比 | enum: ["1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"]。default: "1:1" |
| `resolution` | string | 分辨率 | enum: ["1k", "2k", "4k"]，enumNames: ["标清 1K", "高清 2K", "超清 4K"]。default: "1k" |
| `modules` | array | 模块选择（多选） | enum 及 enumNames 见下方模块列表。minItems: 1 |

**模块列表（enum / enumNames）：**

| enum | enumName | 描述 |
|------|----------|------|
| `hero_kv` | 首屏 KV | 建立第一眼识别，吸引点击 |
| `full_display` | 整体展示 | 完整形态与高级氛围 |
| `detail_closeup` | 细节特写 | 放大材质与工艺质感 |
| `usage_scenario` | 使用场景 | 呈现真实使用状态 |
| `multi_color` | 多色套装 | 展示多 SKU 与组合美感 |
| `feature_compare` | 功能对比 | 参数、功效与差异说明 |
| `packaging` | 包装展示 | 礼盒、配件与开箱细节 |
| `warranty` | 权益保障 | 售后、质保与信任背书 |

## Step 2: 宽高比与参数映射

在调用 `image_generate` 之前，将用户选择的宽高比和分辨率映射为实际参数：

### 宽高比 → size 映射

| aspect_ratio | size |
|-------------|------|
| 1:1 | 1024x1024 |
| 16:9 | 1792x1024 |
| 9:16 | 1024x1792 |
| 4:3 | 1440x1024 |
| 3:4 | 1024x1440 |
| 3:2 | 1536x1024 |
| 2:3 | 1024x1536 |

### 分辨率 → quality 映射

| resolution | quality |
|-----------|---------|
| 1k | standard |
| 2k | high |
| 4k | high |

如果 resolution 为 "4k"，在 prompt 末尾追加 `, 4K ultra high resolution, extremely detailed`。

## Step 3: 生成提示词

根据用户的选择，为每个选中的模块生成一条专业提示词。

### 提示词生成规则

每条提示词由三部分组成，用英文撰写（如 language 为 zh 则在末尾加中文风格词）：

```
[产品描述] + [场景指令] + [风格与质量词]
```

### 版本风格差异（关键）

**实用版 (practical):**
- 风格词：clean product photography, studio lighting, white background, commercial product shot, e-commerce ready
- 强调：清晰还原产品原貌，干净利落

**大片版 (cinematic):**
- 风格词：cinematic lighting, luxury aesthetic, premium commercial photography, shallow depth of field, editorial quality, high-end retouching
- 强调：氛围感、品牌高级感、情感共鸣

### 各模块场景指令

**首屏 KV (hero_kv):**
- 构图：产品居中偏上，占画面 60-70%
- 灯光：戏剧化布光，强调轮廓
- 背景：渐变或品牌色背景
- 实用版示例 prompt：
  > Clean product photography of [产品], centered composition on gradient background, studio lighting with soft shadows, e-commerce hero image, white background edges, 8K, commercial quality
- 大片版示例 prompt：
  > Cinematic hero shot of [产品], dramatic rim lighting, floating on a dark gradient backdrop with subtle reflections, luxury brand aesthetic, shallow depth of field, editorial photography, premium retouching, 8K

**整体展示 (full_display):**
- 构图：产品完整正面 + 侧面/45度角
- 强调：整体形态、比例、设计语言
- 实用版示例：
  > Product photography of [产品], front three-quarter view on white seamless background, even studio lighting, showing full design and proportions, commercial catalog style
- 大片版示例：
  > [产品] suspended in minimalist space, soft directional light revealing sculptural form, subtle caustics and shadows, architectural product photography, high-end catalog, 8K

**细节特写 (detail_closeup):**
- 构图：微距特写，产品局部占满画面
- 强调：材质纹理、工艺细节、品质感
- 实用版示例：
  > Macro detail shot of [产品] texture and craftsmanship, sharp focus on material surface, studio lighting, product detail photography
- 大片版示例：
  > Extreme macro close-up of [产品], revealing intricate textures and precision craftsmanship, shallow depth of field, cinematic lighting with golden highlights, luxury detail photography, 8K

**使用场景 (usage_scenario):**
- 构图：产品在真实环境中的使用
- 强调：生活方式、使用体验
- 实用版示例：
  > Lifestyle product photography of [产品] in a modern clean interior setting, natural window light, real usage context, relatable lifestyle scene
- 大片版示例：
  > [产品] in an aspirational lifestyle setting, golden hour natural light, warm atmospheric interior, candid yet polished moment, editorial lifestyle photography, 8K

**多色套装 (multi_color):**
- 构图：多个颜色变体并排或阶梯排列
- 强调：色彩对比、系列感
- 实用版示例：
  > Product lineup of [产品] in multiple color variants, arranged in a clean row on white surface, even lighting, color comparison display, catalog photography
- 大片版示例：
  > Curated color palette lineup of [产品], artfully arranged on a minimalist plinth, soft directional light creating depth, coordinated color story, premium collection photography, 8K

**功能对比 (feature_compare):**
- 构图：前后对比或标注式展示
- 强调：功能效果、差异
- 实用版示例：
  > Product feature demonstration of [产品], before-after comparison layout, clean infographic style, technical but approachable, e-commerce feature display
- 大片版示例：
  > Premium product capability showcase of [产品], dramatic side-by-side visual comparison, sleek technical aesthetic, cinematic lighting emphasizing transformation, 8K

**包装展示 (packaging):**
- 构图：包装盒+产品+配件的开箱式布局
- 强调：开箱体验、精致感
- 实用版示例：
  > Product packaging photography of [产品], unboxing flat lay with box and accessories, clean white background, e-commerce packaging display
- 大片版示例：
  > Premium unboxing experience of [产品], artfully arranged packaging with satin ribbons and inserts, soft top-down lighting, luxury gift box aesthetic, editorial flat lay, 8K

**权益保障 (warranty):**
- 构图：产品+保障标识/图标的信息图风格
- 强调：信任感、专业服务
- 实用版示例：
  > Product with warranty and service badges, clean layout with guarantee icons, trustworthy and professional, e-commerce trust signals display
- 大片版示例：
  > Premium product with elegantly designed service assurance elements, sophisticated badge design, conveying trust and exclusivity, high-end brand after-sales visual, 8K

### 提示词组装

对每个选中的模块：
1. 将设计简报中的产品描述替换模板中的 `[产品]`
2. 如果有促销信息，自然地融入场景（如添加折扣标签、限时标识等视觉元素）
3. 根据版本选择对应的风格词
4. 根据分辨率追加清晰度词
5. 如 language 为 "zh"，在末尾追加中文风格提示（如：适合中国电商平台、中文排版友好）

## Step 4: 确认提示词

用 `ask_user` 工具（display_mode: "form"）展示所有生成的提示词，让用户逐条确认或修改。

表单字段：
- key: `confirmed`，type: boolean，title: "提示词确认"，description: "确认无误，开始生成图片"
- 每张图一个 key 为 `prompt_N`（N 从 0 开始）的 string 字段，title 为模块中文名，default 为生成的提示词文本。用户可以直接修改。

## Step 5: 批量生成图片

用户确认后，对每条确认的提示词调用 `image_generate` 工具：

```
image_generate(
  prompt: <确认后的提示词>,
  size: <映射后的 size>,
  quality: <映射后的 quality>,
  output_format: "png"
)
```

关键点：
- **一定要传入 size 参数**，从 Step 2 的映射表中取值
- 逐条调用，每次调用后等待结果
- 如果某张失败，记录失败信息并继续生成下一张
- 全部完成后，总结生成结果：成功几张、失败几张

## 注意事项

- 设计简报和促销信息可能为空，此时提示词中省略对应部分
- 模块至少选择 1 个
- 主图版本为必选，如果用户未选则默认为"大片版"
- 提示词始终用英文撰写（生图模型对英文理解更好），中文需求以风格词形式融入
- 确认步骤中，用户可以修改任意提示词的内容
- 生成图片后，简要描述每张图的用途，方便用户下载使用
