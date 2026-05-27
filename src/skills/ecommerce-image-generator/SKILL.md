# 电商主图提示词生成器

通过结构化表单收集需求，生成每张图片的专业提示词，经用户确认后批量调用 `image_generate` 出图。

## 触发行为

用户明确说"帮我生成电商图"、"做几张主图"等，此时用户意图明确，**不要做任何额外操作**（如引导上传到电商系统、要求补齐商品信息等）。直接进入核心流程，用 ask_user 表单收集需求后生成图片。

**需求分流：**
- 如果用户提到"主图"、"首图"、"商品图"、"SKU 图片" → 进入「主图生成流程」
- 如果用户提到"详情页"、"详情图"、"详情页模块"、"店铺装修" → 进入「电商详情页图生成流程」

## 核心流程：主图生成（严格按顺序执行，不可跳过任何步骤）

```
Step 1: 收集需求（两个 ask_user 表单）
Step 2: 参数映射
Step 3: 生成提示词
Step 4: 用户确认（必须！用 ask_user 表单）
Step 5: 批量生图
```

**关键约束：Step 4 是强制步骤。未获得用户确认前，绝对不要调用 image_generate。**

## Step 1: 收集需求

**分两次调用 `ask_user` 工具（display_mode: "form"），必须等待第一次返回后再发起第二次。**

### 第一次 ask_user：产品与营销信息

| key | type | title | 说明 |
|-----|------|-------|------|
| `design_brief` | string | 设计简报 | 多行文本。产品核心卖点、USP、视觉方向、希望强调的卖点。maxLength: 300 |
| `promotion_info` | string | 促销信息 | 多行文本。促销活动详情：折扣力度、活动名称、优惠信息等。maxLength: 300 |
| `language` | string | 输出语言 | enum: ["zh", "en"], enumNames: ["中文", "English"] |

### 第二次 ask_user：图像配置与模块选择

**必须在第一次 ask_user 返回后，再发起第二次 ask_user。**

| key | type | title | 说明 |
|-----|------|-------|------|
| `version` | string | 主图版本（必选） | enum: ["practical", "cinematic"], enumNames: ["实用版 - 清晰还原，快速出图", "大片版 - 高级质感，品牌差异，转化率更高"]。required: true |
| `aspect_ratio` | string | 宽高比 | enum: ["1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"]。default: "1:1" |
| `resolution` | string | 分辨率 | enum: ["1k", "2k", "4k"]，enumNames: ["标清 1K", "高清 2K", "超清 4K"]。default: "1k" |
| `modules` | array | 模块选择（多选） | items.anyOf 见下方模块列表。minItems: 1 |

**模块列表（items.anyOf：const 为值，title 为显示名）：**

| const | title | 描述 |
|------|-------|------|
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
5. 提示词语言与用户选择的 `language` 一致：如 "zh" 则用中文撰写，如 "en" 则用英文撰写。风格词部分保持英文以确保生图模型理解准确

## Step 4: 用户确认提示词（强制步骤）

**此步骤不可跳过！在用户确认之前，不得调用 image_generate。**

生成所有提示词后，用 `ask_user` 工具（display_mode: "form"）展示所有提示词，让用户逐条确认或修改。

表单字段：
- key: `confirmed`，type: boolean，title: "提示词确认"，description: "确认无误，开始生成图片"。required: true
- 每张图一个 key 为 `prompt_N`（N 从 0 开始）的 string 字段，title 为模块中文名，default 为生成的提示词文本，multiline: true。用户可以直接修改。

**只有当用户填写表单且 confirmed 为 true 时，才能进入 Step 5。** 如果 confirmed 为 false 或用户未确认，不要生图。

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
- 提示词语言与用户选择的 `language` 一致："zh" 用中文，"en" 用英文。风格词部分保持英文以确保生图模型理解准确
- 确认步骤中，用户可以修改任意提示词的内容
- 生成图片后，简要描述每张图的用途，方便用户下载使用

---

## 电商详情页图生成

当用户明确要求生成"详情页"、"详情图"或选择详情页模块时，执行以下流程。

### 详情页核心流程（严格按顺序执行，不可跳过任何步骤）

```
Step 1: 收集需求（三次 ask_user 表单）
Step 2: 参数映射
Step 3: 生成详情页规划方案
Step 4: 生成各模块提示词
Step 5: 用户确认（必须！用 ask_user 表单）
Step 6: 批量生图
```

**关键约束：Step 5 是强制步骤。未获得用户确认前，绝对不要调用 image_generate。**

### Step 1: 收集需求

**分三次调用 `ask_user` 工具（display_mode: "form"），必须等待前一次返回后再发起下一次。**

#### 第一次 ask_user：产品素材与营销信息

| key | type | title | 说明 |
|-----|------|-------|------|
| `product_images_desc` | string | 已上传参考图 | 用户已上传的产品参考图描述，如"正面白底图2张、场景图1张"。如未上传可填"无"。maxLength: 200 |
| `product_info` | string | 产品信息 | 产品名称、品类、目标人群。maxLength: 100 |
| `design_brief` | string | 设计简报 | 产品核心卖点、USP、视觉方向、希望强调的卖点。maxLength: 300 |
| `promotion_info` | string | 促销信息 | 促销活动详情：折扣力度、活动名称、优惠信息等。maxLength: 300 |

#### 第二次 ask_user：图像配置

**必须在第一次 ask_user 返回后，再发起第二次 ask_user。**

| key | type | title | 说明 |
|-----|------|-------|------|
| `version` | string | 详情页版本（必选） | enum: ["practical", "cinematic"], enumNames: ["实用版 - 清晰还原，快速出图", "大片版 - 高级质感，品牌差异，转化率更高"]。required: true |
| `aspect_ratio` | string | 宽高比 | enum: ["1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"]。default: "3:4" |
| `resolution` | string | 分辨率 | enum: ["1k", "2k", "4k"]，enumNames: ["标清 1K", "高清 2K", "超清 4K"]。default: "1k" |
| `language` | string | 输出语言 | enum: ["zh", "en"], enumNames: ["中文", "English"] |
| `market` | string | 电商市场 | enum: ["domestic", "international"], enumNames: ["国内电商", "国外电商"]。default: "domestic" |

#### 第三次 ask_user：详情页模块选择

**必须在第二次 ask_user 返回后，再发起第三次 ask_user。**

| key | type | title | 说明 |
|-----|------|-------|------|
| `modules` | array | 详情页模块（多选） | items.anyOf 见下方模块列表。minItems: 1 |

**详情页模块列表（items.anyOf：const 为值，title 为显示名）：**

| const | title | 描述 |
|------|-------|------|
| `hero_kv` | 首屏主视觉 | 详情页首屏，建立第一眼识别，吸引继续浏览 |
| `selling_point` | 核心卖点图 | 突出差异化优势与USP，信息层级清晰 |
| `usage_scenario` | 使用场景图 | 呈现真实使用状态，生活方式感 |
| `multi_angle` | 多角度图 | 正面、侧面、45度、背面完整展示外观 |
| `scene_atmosphere` | 场景氛围图 | 精心布置的场景，强调氛围感与情感共鸣 |
| `detail_closeup` | 商品细节图 | 放大材质纹理与工艺质感 |
| `brand_story` | 品牌故事图 | 传达品牌理念、价值观与情感连接 |
| `size_spec` | 尺寸/容量/尺码图 | 规格信息可视化，配合参照物或标注 |
| `before_after` | 效果对比图 | 使用前后对比，增强说服力 |
| `spec_table` | 详细规格/参数表 | 专业数据信息图，清晰可信 |
| `craft_process` | 工艺制作图 | 展示工艺步骤或材质来源，强调匠心 |
| `accessory` | 配件/赠品图 | 开箱式展示所有物品，明确收货内容 |
| `series_display` | 系列展示图 | 多色或多SKU组合展示，强调系列感 |
| `ingredient` | 商品成分图 | 成分/材质/配方可视化，强调安全高品质 |
| `warranty` | 售后保障图 | 质保、退换政策说明，建立信任背书 |
| `usage_guide` | 使用建议图 | 使用步骤或场景建议，简单易懂 |
| `buyer_show` | 买家秀 | 真实用户视角，增强可信度 |
| `blogger_rec` | 博主推荐图 | 达人种草/测评推荐风格，社交传播感 |

### Step 2: 宽高比与参数映射

与主图生成流程的映射规则一致：

| aspect_ratio | size |
|-------------|------|
| 1:1 | 1024x1024 |
| 16:9 | 1792x1024 |
| 9:16 | 1024x1792 |
| 4:3 | 1440x1024 |
| 3:4 | 1024x1440 |
| 3:2 | 1536x1024 |
| 2:3 | 1024x1536 |

| resolution | quality |
|-----------|---------|
| 1k | standard |
| 2k | high |
| 4k | high |

如果 resolution 为 "4k"，在 prompt 末尾追加 `, 4K ultra high resolution, extremely detailed`。

### Step 3: 生成详情页规划方案

在生成提示词之前，先根据用户选择的模块生成一份**详情页规划方案**。方案用 markdown 格式输出，包含以下内容：

1. **整体视觉方向**：基于 version 和 market 确定的整体风格
2. **模块排布逻辑**：各模块的推荐排序与衔接关系
3. **逐模块规划**：每个选中模块的内容重点、构图建议、与前后模块的衔接

### Step 4: 生成各模块提示词

根据用户的选择，为每个选中的模块生成一条专业提示词。

#### 提示词生成规则

每条提示词由三部分组成，用英文撰写（如 language 为 zh 则在末尾加中文风格词）：

```
[产品描述] + [场景指令] + [风格与质量词]
```

#### 版本风格差异（同主图流程）

**实用版 (practical):**
- 风格词：clean product photography, studio lighting, white background, commercial product shot, e-commerce ready
- 强调：清晰还原产品原貌，干净利落

**大片版 (cinematic):**
- 风格词：cinematic lighting, luxury aesthetic, premium commercial photography, shallow depth of field, editorial quality, high-end retouching
- 强调：氛围感、品牌高级感、情感共鸣

#### 各模块场景指令

**首屏主视觉 (hero_kv):**
- 构图：产品居中偏上，占画面 60-70%，下方或侧面留白放文案
- 背景：渐变或品牌色背景，简洁大气
- 实用版示例：
  > Clean product photography of [产品], centered composition on gradient background, studio lighting with soft shadows, e-commerce hero banner, white background edges, 8K, commercial quality
- 大片版示例：
  > Cinematic hero shot of [产品], dramatic rim lighting, floating on a dark gradient backdrop with subtle reflections, luxury brand aesthetic, shallow depth of field, editorial photography, premium retouching, 8K

**核心卖点图 (selling_point):**
- 构图：产品+信息层级区域，卖点可视化
- 强调：差异化优势、USP 突出
- 实用版示例：
  > Product photography of [产品] with clean infographic-style layout, highlighting key selling points, white background, studio lighting, e-commerce feature display, commercial catalog style
- 大片版示例：
  > Premium capability showcase of [产品], dramatic lighting emphasizing transformation and benefits, sleek technical aesthetic, luxury brand visual, editorial quality, 8K

**使用场景图 (usage_scenario):**
- 同主图流程的 usage_scenario 模块

**多角度图 (multi_angle):**
- 构图：产品完整正面 + 侧面/45度角/背面组合展示
- 强调：整体形态、比例、设计语言完整呈现
- 实用版示例：
  > Product photography of [产品], front three-quarter and side view composite on white seamless background, even studio lighting, showing full design and proportions, commercial catalog style
- 大片版示例：
  > [产品] suspended in minimalist space, multiple angles visible, soft directional light revealing sculptural form, subtle caustics and shadows, architectural product photography, high-end catalog, 8K

**场景氛围图 (scene_atmosphere):**
- 构图：产品在精心布置的生活方式场景中
- 强调：氛围感、情感共鸣、生活方式代入
- 实用版示例：
  > Lifestyle product photography of [产品] in a modern clean interior setting, natural window light, warm atmospheric scene, relatable lifestyle context
- 大片版示例：
  > [产品] in an aspirational lifestyle setting, golden hour natural light, warm atmospheric interior, candid yet polished moment, editorial lifestyle photography, 8K

**商品细节图 (detail_closeup):**
- 同主图流程的 detail_closeup 模块

**品牌故事图 (brand_story):**
- 构图：品牌元素+产品，有叙事感和情感温度
- 强调：品牌理念、价值观、情感连接
- 实用版示例：
  > Brand story visual of [产品] with brand elements, clean layout, professional studio lighting, trustworthy and approachable, e-commerce brand display
- 大片版示例：
  > Premium brand narrative of [产品], artfully composed with brand heritage elements, cinematic lighting, conveying trust and exclusivity, high-end brand visual, 8K

**尺寸/容量/尺码图 (size_spec):**
- 构图：产品+尺寸标注或日常参照物对比
- 强调：规格清晰、直观易懂
- 实用版示例：
  > Product size specification photography of [产品] with dimension labels and everyday reference objects, clean white background, infographic style, e-commerce specification display
- 大片版示例：
  > Elegant size comparison of [产品] with curated props, soft studio lighting, minimalist composition, premium catalog aesthetic, 8K

**效果对比图 (before_after):**
- 构图：使用前后对比，分屏或并列布局
- 强调：效果差异、说服力、直观对比
- 实用版示例：
  > Before-after comparison of [产品] usage, split-screen layout, clean white background, even lighting, e-commerce comparison display
- 大片版示例：
  > Dramatic before-after transformation with [产品], cinematic lighting emphasizing contrast, sleek visual composition, premium retouching, 8K

**详细规格/参数表 (spec_table):**
- 构图：产品+参数信息的专业排版
- 强调：数据清晰、专业可信、易于阅读
- 实用版示例：
  > Product specification photography of [产品] with clean parameter table layout, professional infographic style, white background, e-commerce technical display
- 大片版示例：
  > Premium technical showcase of [产品] with elegantly designed data visualization, sophisticated layout, cinematic lighting, high-end brand visual, 8K

**工艺制作图 (craft_process):**
- 构图：工艺步骤展示或材质来源特写
- 强调：匠心品质、制作精细、材质高级
- 实用版示例：
  > Craftsmanship photography of [产品] showing production process or material close-up, clean studio lighting, professional manufacturing display
- 大片版示例：
  > Artisan craftsmanship showcase of [产品], revealing intricate making process, cinematic lighting with golden highlights, luxury detail photography, 8K

**配件/赠品图 (accessory):**
- 构图：产品+所有配件/赠品的开箱式平铺展示
- 强调：完整开箱、物超所值、精致感
- 实用版示例：
  > Product unboxing flat lay of [产品] with all accessories and gifts, clean white background, e-commerce packaging display
- 大片版示例：
  > Premium unboxing experience of [产品], artfully arranged with satin ribbons and inserts, soft top-down lighting, luxury gift box aesthetic, editorial flat lay, 8K

**系列展示图 (series_display):**
- 同主图流程的 multi_color 模块

**商品成分图 (ingredient):**
- 构图：成分/材质/配方的可视化展示
- 强调：安全、天然、高品质、透明可信
- 实用版示例：
  > Product ingredient visualization of [产品], clean infographic style with material breakdown, white background, professional e-commerce display
- 大片版示例：
  > Premium ingredient showcase of [产品], artfully composed with natural elements, soft cinematic lighting, conveying purity and quality, luxury brand visual, 8K

**售后保障图 (warranty):**
- 同主图流程的 warranty 模块

**使用建议图 (usage_guide):**
- 构图：使用步骤展示或场景建议
- 强调：简单易懂、贴心指导、降低决策门槛
- 实用版示例：
  > Product usage guide photography of [产品], step-by-step demonstration, clean white background, approachable instructional style, e-commerce guide display
- 大片版示例：
  > Premium usage inspiration of [产品] in aspirational scenarios, cinematic lifestyle photography, conveying ease and elegance, editorial quality, 8K

**买家秀 (buyer_show):**
- 构图：真实用户视角的产品照片，生活化场景
- 强调：真实感、可信赖、降低购买顾虑
- 实用版示例：
  > Authentic user-generated style photo of [产品] in real home setting, natural lighting, candid composition, relatable everyday scene
- 大片版示例：
  > Polished lifestyle photo of [产品] in aspirational yet realistic setting, natural window light, candid moment, editorial lifestyle photography, 8K

**博主推荐图 (blogger_rec):**
- 构图：达人/博主风格的产品展示，种草感
- 强调：专业推荐、社交传播感、信任背书
- 实用版示例：
  > Influencer-style product photo of [产品], clean bright aesthetic, approachable recommendation vibe, social media ready, e-commerce influencer display
- 大片版示例：
  > Premium influencer showcase of [产品], artfully styled with lifestyle props, cinematic lighting, aspirational yet authentic, editorial social content, 8K

#### 提示词组装

对每个选中的模块：
1. 将设计简报中的产品描述替换模板中的 `[产品]`
2. 如果有促销信息，自然地融入场景
3. 根据 version 选择对应的风格词
4. 根据 resolution 追加清晰度词
5. 提示词语言与用户选择的 `language` 一致：如 "zh" 则用中文撰写，如 "en" 则用英文撰写。风格词部分保持英文以确保生图模型理解准确
6. 如 market 为 "domestic"，融入国内电商审美（如：淘宝/京东风格、中式审美）；如为 "international"，融入国际化审美（如：Amazon/Shopify 风格、极简国际化）

### Step 5: 用户确认提示词（强制步骤）

**此步骤不可跳过！在用户确认之前，不得调用 image_generate。**

生成规划方案和所有提示词后，用 `ask_user` 工具（display_mode: "form"）展示以下内容，让用户确认或修改：

1. **详情页规划方案摘要**（markdown 文本）
2. **逐模块提示词**：每张图一个 key 为 `prompt_N`（N 从 0 开始）的 string 字段，title 为模块中文名，default 为生成的提示词文本，multiline: true
3. **确认字段**：key: `confirmed`，type: boolean，title: "提示词确认"，description: "确认无误，开始生成图片"。required: true

**只有当用户填写表单且 confirmed 为 true 时，才能进入 Step 6。**

### Step 6: 批量生成图片

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
- 全部完成后，总结生成结果：成功几张、失败几张，并说明每张图对应的详情页模块用途

### 详情页生成注意事项

- 设计简报和促销信息可能为空，此时提示词中省略对应部分
- 模块至少选择 1 个，建议 3-8 个以形成完整详情页
- 详情页版本为必选，如果用户未选则默认为"大片版"
- 提示词语言与用户选择的 `language` 一致："zh" 用中文，"en" 用英文。风格词部分保持英文以确保生图模型理解准确
- 市场选择（国内/国外）会影响审美风格和场景元素
- 确认步骤中，用户可以修改任意提示词的内容
- 生成图片后，按详情页模块顺序简要描述每张图的用途，方便用户按顺序使用
