# GeoGebra Drawing

GeoGebra 交互式数学可视化 Skill。生成 GGBScript 代码块，支持函数绘图、几何作图、坐标系构建和动态交互。

## 输出格式

使用 `ggbscript` 围栏代码块，每行一条命令，`#` 开头为注释。

````
```ggbscript
# 注释
A = (1, 2)
f(x) = x^2
```
````

## GGBScript 命令参考

### 点与坐标

```
A = (1, 2)                          # 自由点
B = (3, 4)
M = Midpoint(A, B)                  # 中点
P = Point({2, 3})                   # 从列表创建点
C = (A + B) / 2                     # 向量运算取中点
O = (0, 0)                          # 原点
P_on = Point(f)                     # 曲线上的点
Intersect(f, g)                     # 交点
Intersect(f, g, 1)                  # 第 n 个交点
```

### 直线与线段

```
l = Line(A, B)                      # 过两点的直线
s = Segment(A, B)                   # 线段
r = Ray(A, B)                       # 射线
v = Vector(A, B)                    # 向量
p = PerpendicularLine(C, l)         # 过 C 垂直于 l
q = PerpendicularLine(A, Segment(B, C))
h = LineBisector(A, B)              # 中垂线
par = Line(A, y = 2x + 1)          # 过 A 平行于给定直线
ang = AngleBisector(A, B, C)        # 角平分线
```

### 函数与曲线

```
f(x) = x^2                          # 基本函数
g(x) = sin(x)
h(x) = Function(x^2, -3, 3)        # 限定定义域 [-3, 3]
f'(x) = Derivative(f)               # 导函数
F(x) = Integral(f)                  # 不定积分
area = Integral(f, 1, 3)            # 定积分 (带阴影)
IntegralBetween(f, g, a, b)         # 两函数间面积
t = Tangent(2, f)                   # x=2 处切线
t2 = Tangent(A, f)                  # 过点 A 的切线
asym = Asymptote(f)                 # 渐近线
root = Root(f)                      # 零点
ext = Extremum(f)                   # 极值点
infl = InflectionPoint(f)           # 拐点
```

### 圆锥曲线

```
c = Circle(A, 3)                    # 圆心 A 半径 3
c2 = Circle(A, B)                   # 以 AB 为半径
c3 = Circle(A, B, C)                # 过三点的圆
e = Ellipse(F1, F2, 5)              # 椭圆 (焦点 + 半长轴)
hyp = Hyperbola(F1, F2, 3)          # 双曲线
par = Parabola(F, l)                # 抛物线 (焦点 + 准线)
Conic(A, B, C, D, E)                # 过五点的圆锥曲线
```

### 多边形

```
poly = Polygon(A, B, C)             # 三角形
quad = Polygon(A, B, C, D)          # 四边形
reg = Polygon(A, B, 6)              # 以 AB 为边的正六边形
```

### 变换

```
A' = Translate(A, v)                # 平移
B' = Rotate(A, 45°, O)             # 绕 O 旋转 45 度
C' = Reflect(A, l)                  # 关于直线 l 对称
D' = Reflect(A, O)                  # 关于点 O 对称
E' = Dilate(A, 2, O)               # 以 O 为中心缩放 2 倍
```

### 样式设置

```
SetColor(f, "Red")                   # 预定义颜色
SetColor(A, 0, 0, 255)              # RGB 值
SetLineThickness(f, 5)              # 线宽 1-13
SetLineStyle(f, 1)                  # 0=实线 1=虚线 2=点线 3=点划线
SetFilling(c, 0.3)                  # 填充透明度 0-1
SetPointSize(A, 5)                  # 点大小 1-9
SetPointStyle(A, 1)                 # 点样式
SetLabelVisible(A, true)            # 标签可见性
SetFixed(A, true)                   # 固定对象
ShowLabel(A, true)
SetCaption(A, "起点")
ZoomIn(-10, -10, 10, 10)            # 设置可视区域
ShowAxes(true)
ShowGrid(true)
SetAxesRatio(1, 1)                  # 坐标轴比例
```

### 文本与标注

```
text1 = Text("hello")                         # 纯文本
text2 = Text("面积 = " + area)                # 动态文本
tex = FormulaText(f)                           # LaTeX 公式
alpha = Angle(A, B, C)                         # 角度
d = Distance(A, B)                             # 距离
len = Length(s)                                 # 线段长度
```

### 交互元素

```
a = Slider(0, 10, 0.1)                        # 滑块 (最小值, 最大值, 步长)
SetConditionToShowObject(A, a > 5)             # 条件显示
SetValue(a, 3)                                 # 设置滑块值
StartAnimation(a)                              # 启动动画
```

## 使用指南

- 输出纯 GGBScript，不要混入其他语言
- 对象命名清晰，几何点用大写字母，函数用小写字母
- 用 `#` 注释标注构图步骤
- 按逻辑顺序构建：先定义基础对象，再构建依赖对象，最后设置样式
- 复杂图形分步构建，每步注释说明意图
- 角度单位使用度数符号 `°`
- 涉及坐标系时主动调用 `ZoomIn` 设置合适的可视区域

## 适用场景

- 函数图像绘制与分析（导数、积分、极值、渐近线）
- 平面几何作图（三角形、圆、多边形及其性质）
- 解析几何（圆锥曲线、直线方程、交点求解）
- 几何变换演示（平移、旋转、对称、缩放）
- 动态交互演示（滑块控制参数变化）
- 数学教学辅助可视化

## 限制

- 仅支持 2D 平面作图，不支持 3D
- GGBScript 不支持条件分支和循环，复杂逻辑需拆分为多条命令
- 颜色名称仅支持 GeoGebra 内置预定义颜色（Red, Blue, Green, Orange 等）或 RGB 值
- 动画和交互依赖 GeoGebra 运行时环境
