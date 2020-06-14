#!/usr/bin/env python3

# Dependencies: python3-pandas python3-plotly

import pandas as pd
import plotly.graph_objects as go
from plotly.subplots import make_subplots
import plotly.colors

df = pd.read_csv(".perf-out/all.csv")

fig = make_subplots(
    rows=2, cols=2,
    horizontal_spacing = 0.1,
    vertical_spacing = 0.1,
    subplot_titles=(
        "Requests per second",
        "Latency: 90%ile", "Latency: 99%ile", "Latency: 99.9%ile"),
)

fig.update_yaxes(row=1, col=1, rangemode="tozero")
fig.update_yaxes(row=1, col=2, title_text="milliseconds",
        rangemode="tozero")
fig.update_yaxes(row=2, col=1, title_text="milliseconds",
        rangemode="tozero")
fig.update_yaxes(row=2, col=2, title_text="milliseconds",
        rangemode="tozero")

fig.update_layout(legend_orientation="h", hovermode="x")

colors = plotly.colors.DEFAULT_PLOTLY_COLORS
for i, s in enumerate(set(df.server.values)):
    dfs = df[df.server == s]
    color = colors[i]

    fig.add_trace(
            go.Scatter(
                x=dfs["size"],
                y=dfs.reqps,
                mode='lines+markers',
                line=dict(color=color),
                showlegend=True,
                name=s),
            row=1, col=1)

    for (row, col), k in [
            ((1, 2), "lat90"),
            ((2, 1), "lat99"),
            ((2, 2), "lat99.9")]:
        fig.add_trace(
                go.Scatter(
                    x=dfs["size"],
                    y=dfs[k]/1000,  # convert us -> ms
                    mode='lines+markers',
                    line=dict(color=color),
                    showlegend=False,
                    name=s),
                row=row, col=col)


fig.write_html('.perf-out/results.html', auto_open=False)

