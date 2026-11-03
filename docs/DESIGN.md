# 前端设计系统

> 参考对象：**HiDNS**（`/data/dsh/workspace/HiDNS`，root.ai.md「客户端架构」「主题与背景」两节）。
> 说明：hidoha.net 站点被 Cloudflare 拦截（403），因此这里对齐的是 HiDNS **仓库内成文的规范**，
> 而不是截图复刻；视觉参数集中在本仓库 `client/src/ui/tokens.css`，换参考对象只改这一个文件。

## 1. 技术选型（与 HiDNS 同构）

| 维度 | 选择 | 理由 |
|---|---|---|
| 框架 | React 18 + TypeScript + Vite | 与 HiDNS 一致；路由级代码分割，构建产物 250 kB / gzip 79 kB |
| 路由 | react-router-dom 6 | 守卫用嵌套 `<Route element>`（`ProtectedShell` → `AppShell`） |
| 服务端状态 | TanStack Query v5 | 轮询与缓存失效交给 query，不在组件里手写 setInterval |
| 样式 | Tailwind 3.4 + CSS 变量 tokens | 语义色走 HSL 变量，主题切换只切 `.dark` |
| 原语 | 自建 `ui/`（Button/Input/Card/Badge/Skeleton） | **不引入整包 UI 框架**（HiDNS 硬性约束），保持可控与体积 |
| 图标 | lucide-react | 与 HiDNS 相同，避免再引入第二个图标集 |
| 语言 | 单一简体中文，文案集中在 `copy/zh-CN.ts` | HiDNS 同类决策：不做 i18n 框架 |

目录约定：`app/`（外壳与守卫）、`ui/`（设计原语）、`lib/`（http/theme）、
`copy/`（文案）、`features/<domain>/`（页面只能写在这里）。

## 2. 黄金比例几何

```css
--phi: 1.618034;
--u-1: 5px;  --u-2: 6px;  --u-3: 8px;  --u-4: 10px;
--u-5: 13px; --u-6: 16px; --u-7: 21px; --u-8: 27px;
--u-9: 34px; --u-10: 43px; --u-11: 55px; --u-12: 70px;
```

每一档都是 `8 × φ^(n/2)`，所以「间距 / 圆角 / 字号」共享同一比例，
不需要额外的排版尺度表。落点：

- 字体：`phi-sm 13px` / `phi-base 16px` / `phi-lg 21px` / `phi-xl 27px` / `phi-2xl 34px` / `phi-3xl 55px`
- 布局：侧边栏 `--w-sidebar`，正文最大宽 `--w-content`（≈1252px）
- **主分栏 61.8% / 38.2%**：`.phi-split` 用于登录页（品牌区 : 表单区），
  网络页的「列表 : 新建」两栏用 `lg:grid-cols-[1fr_0.618fr]`
- 行高统一 1.618（正文）/ 1.382（标题）

## 3. 自定义全局背景

四层结构（与 HiDNS 的 L1–L4 分层同构）：

| 层 | 变量 | 作用 |
|---|---|---|
| L1 画面 | `--app-bg-image` 或内置 `.app-backdrop::before` | 用户图 / 三道程序化极光 |
| L2 蒙层 | `--scrim-rgb` + `--scrim-alpha` | 保证任何底图上文字对比度可控 |
| L3 结构面 | `--panel-alpha` / `--chrome-alpha` + `--chrome-blur` | 卡片与顶栏 |
| L4 内容 | 语义色 | 文本与控件 |

设计取舍：

1. **默认不请求第三方图源**。HiDNS 默认可拉随机图，但控制台常常部署在内网，
   默认值必须是「零网络依赖」的极光渐变；想用图就填地址。
2. 用户背景地址只接受 `http(s)`，写入 CSS 前用 `JSON.stringify` 包裹
   （`url("...")`），挡掉 `javascript:` 与引号注入。
3. 蒙层透明度 `0.72` 是「图仍然可见」与「正文对比度 ≥ 4.5:1」的交点；
   面板保持接近实色，避免同页两张卡因底图不同而色偏。

## 4. 主题

`lib/theme.tsx` 是唯一来源，包裹整个应用（含登录页）。存储键 `theme`，
取值 `light | dark | auto`；`index.html` 内联脚本在首屏前应用，
深色用户不会看到白闪。运行时切换只改 `<html class="dark">` 与 `color-scheme`。.

## 5. 可访问性

- 语义色按 WCAG AA 校准：正文对 `--background` ≥ 4.5:1，控件边界对卡片 ≥ 3:1。
- 全站统一 `:focus-visible` 双像素描边，键盘可达。
- 状态不只靠颜色：设备在线/离线同时输出文字徽标。
- 表格在窄屏降级为列表（后续迭代；当前用横向滚动兜底）。
