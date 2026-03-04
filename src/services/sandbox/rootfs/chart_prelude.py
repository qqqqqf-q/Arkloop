# Arkloop Chart Prelude
#
# 由 sandbox agent 在检测到图表库使用时自动注入。
# 功能：透明背景 + Arkloop 配色模板 + 品牌水印注入。

_ARKLOOP_BRAND = "Powered by Arkloop"

_COLOR_TEXT = "#737373"
_COLOR_GRID = "rgba(115,115,115,0.15)"
_COLOR_AXIS = "rgba(115,115,115,0.25)"
_COLOR_BRAND = "rgba(115,115,115,0.5)"
_COLORWAY = ["#4ECDC4", "#FF6B6B", "#45B7D1", "#96E6A1", "#DDA0DD", "#FFB347"]
_FONT_FAMILY = "Noto Sans CJK SC, Inter, -apple-system, system-ui, sans-serif"
_BRAND_FONT = "Inter, -apple-system, system-ui, sans-serif"

_MPL_COLOR_GRID = (0.45, 0.45, 0.45, 0.15)
_MPL_COLOR_AXIS = (0.45, 0.45, 0.45, 0.25)
_MPL_COLOR_BRAND = (0.45, 0.45, 0.45, 0.5)


def _add_plotly_brand(fig):
    """向 Plotly figure 注入品牌水印（右下角）。"""
    has_brand = any(
        getattr(a, "text", None) == _ARKLOOP_BRAND
        for a in (fig.layout.annotations or [])
    )
    if not has_brand:
        fig.add_annotation(
            text=_ARKLOOP_BRAND,
            xref="paper",
            yref="paper",
            x=1.0,
            y=-0.12,
            showarrow=False,
            font=dict(size=11, color=_COLOR_BRAND, family=_BRAND_FONT),
            xanchor="right",
            yanchor="top",
        )


def _setup_plotly():
    import plotly.graph_objects as go
    import plotly.io as pio

    arkloop_layout = go.Layout(
        paper_bgcolor="rgba(0,0,0,0)",
        plot_bgcolor="rgba(0,0,0,0)",
        font=dict(color=_COLOR_TEXT, size=14, family=_FONT_FAMILY),
        colorway=_COLORWAY,
        title=dict(font=dict(color=_COLOR_TEXT, size=22)),
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
        margin=dict(b=80),
    )
    pio.templates["arkloop"] = go.layout.Template(layout=arkloop_layout)
    pio.templates.default = "arkloop"

    _orig_write_image = go.Figure.write_image
    _orig_write_html = go.Figure.write_html
    _orig_to_html = go.Figure.to_html

    def _write_image_with_brand(self, *args, **kwargs):
        _add_plotly_brand(self)
        kwargs.setdefault("scale", 3)
        kwargs.setdefault("width", 900)
        kwargs.setdefault("height", 550)
        try:
            return _orig_write_image(self, *args, **kwargs)
        except Exception:
            # kaleido/Chrome 失败时降级为 HTML
            import sys, os
            path = args[0] if args else kwargs.get("file")
            if path and isinstance(path, str):
                html_path = os.path.splitext(path)[0] + ".html"
                _orig_write_html(self, html_path)
                print(f"[arkloop] write_image failed, saved as {html_path}", file=sys.stderr)
                return
            raise

    def _write_html_with_brand(self, *args, **kwargs):
        _add_plotly_brand(self)
        return _orig_write_html(self, *args, **kwargs)

    def _to_html_with_brand(self, *args, **kwargs):
        _add_plotly_brand(self)
        return _orig_to_html(self, *args, **kwargs)

    go.Figure.write_image = _write_image_with_brand
    go.Figure.write_html = _write_html_with_brand
    go.Figure.to_html = _to_html_with_brand


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
            "axes.titlesize": 16,
            "axes.labelsize": 13,
            "xtick.color": _COLOR_TEXT,
            "xtick.labelsize": 12,
            "ytick.color": _COLOR_TEXT,
            "ytick.labelsize": 12,
            "axes.edgecolor": _MPL_COLOR_AXIS,
            "axes.spines.top": False,
            "axes.spines.right": False,
            "grid.color": _MPL_COLOR_GRID,
            "grid.alpha": 0.5,
            "legend.facecolor": "none",
            "legend.edgecolor": "none",
            "legend.labelcolor": _COLOR_TEXT,
            "legend.fontsize": 11,
            "figure.edgecolor": "none",
            "figure.titlesize": 18,
            "font.size": 13,
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
                0.01,
                _ARKLOOP_BRAND,
                transform=self.transFigure,
                fontsize=9,
                color=_MPL_COLOR_BRAND,
                ha="right",
                va="bottom",
                fontstyle="italic",
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
