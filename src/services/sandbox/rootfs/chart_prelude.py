# Arkloop Chart Prelude
#
# 由 sandbox agent 在检测到图表库使用时自动注入。
# 功能：透明背景 + Arkloop 配色模板 + 品牌水印注入。

_ARKLOOP_BRAND = "Powered by Arkloop"

# 深色 (#1E1D1C) 和浅色 (#F8F8F7) 背景均可读的中性灰
_COLOR_TEXT = "#737373"
_COLOR_GRID = "rgba(115,115,115,0.15)"
_COLOR_AXIS = "rgba(115,115,115,0.25)"
_COLOR_BRAND = "rgba(115,115,115,0.6)"
_COLORWAY = ["#4ECDC4", "#FF6B6B", "#45B7D1", "#96E6A1", "#DDA0DD", "#FFB347"]
_FONT_FAMILY = "Noto Sans CJK SC, Inter, -apple-system, system-ui, sans-serif"

# matplotlib 不支持 CSS rgba()，使用元组格式
_MPL_COLOR_GRID = (0.45, 0.45, 0.45, 0.15)
_MPL_COLOR_AXIS = (0.45, 0.45, 0.45, 0.25)
_MPL_COLOR_BRAND = (0.45, 0.45, 0.45, 0.6)


def _setup_plotly():
    import plotly.graph_objects as go
    import plotly.io as pio

    arkloop_layout = go.Layout(
        paper_bgcolor="rgba(0,0,0,0)",
        plot_bgcolor="rgba(0,0,0,0)",
        font=dict(color=_COLOR_TEXT, family=_FONT_FAMILY),
        colorway=_COLORWAY,
        title=dict(font=dict(color=_COLOR_TEXT, size=20)),
        xaxis=dict(
            gridcolor=_COLOR_GRID,
            linecolor=_COLOR_AXIS,
            zerolinecolor=_COLOR_GRID,
            tickfont=dict(color=_COLOR_TEXT),
            title_font=dict(color=_COLOR_TEXT),
        ),
        yaxis=dict(
            gridcolor=_COLOR_GRID,
            linecolor=_COLOR_AXIS,
            zerolinecolor=_COLOR_GRID,
            tickfont=dict(color=_COLOR_TEXT),
            title_font=dict(color=_COLOR_TEXT),
        ),
        legend=dict(font=dict(color=_COLOR_TEXT)),
    )
    pio.templates["arkloop"] = go.layout.Template(layout=arkloop_layout)
    pio.templates.default = "arkloop"

    _orig_write_image = go.Figure.write_image

    def _write_image_with_brand(self, *args, **kwargs):
        has_brand = any(
            getattr(a, "text", None) == _ARKLOOP_BRAND
            for a in (self.layout.annotations or [])
        )
        if not has_brand:
            self.add_annotation(
                text=_ARKLOOP_BRAND,
                xref="paper",
                yref="paper",
                x=0.98,
                y=1.06,
                showarrow=False,
                font=dict(size=14, color=_COLOR_BRAND, family=_FONT_FAMILY),
                xanchor="right",
                yanchor="bottom",
            )
        kwargs.setdefault("scale", 2)
        return _orig_write_image(self, *args, **kwargs)

    go.Figure.write_image = _write_image_with_brand


def _setup_matplotlib():
    import matplotlib

    matplotlib.rcParams.update(
        {
            "savefig.transparent": True,
            "savefig.dpi": 200,
            "figure.dpi": 200,
            "figure.facecolor": "none",
            "axes.facecolor": "none",
            "text.color": _COLOR_TEXT,
            "axes.labelcolor": _COLOR_TEXT,
            "xtick.color": _COLOR_TEXT,
            "ytick.color": _COLOR_TEXT,
            "axes.edgecolor": _MPL_COLOR_AXIS,
            "grid.color": _MPL_COLOR_GRID,
            "grid.alpha": 0.5,
            "legend.facecolor": "none",
            "legend.edgecolor": "none",
            "legend.labelcolor": _COLOR_TEXT,
            "figure.edgecolor": "none",
            "font.family": "sans-serif",
            "font.sans-serif": [
                "Noto Sans CJK SC",
                "WenQuanYi Micro Hei",
                "SimHei",
                "DejaVu Sans",
                "sans-serif",
            ],
            "axes.unicode_minus": False,
        }
    )

    import matplotlib.figure as _mpl_fig

    _orig_savefig = _mpl_fig.Figure.savefig

    def _savefig_with_brand(self, *args, **kwargs):
        if not getattr(self, "_arkloop_branded", False):
            self.text(
                0.98,
                0.98,
                _ARKLOOP_BRAND,
                transform=self.transFigure,
                fontsize=12,
                color=_MPL_COLOR_BRAND,
                ha="right",
                va="top",
            )
            self._arkloop_branded = True
        kwargs.setdefault("transparent", True)
        return _orig_savefig(self, *args, **kwargs)

    _mpl_fig.Figure.savefig = _savefig_with_brand


try:
    _setup_plotly()
except ImportError:
    pass

try:
    _setup_matplotlib()
except ImportError:
    pass
