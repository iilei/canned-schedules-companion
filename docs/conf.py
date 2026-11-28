# The companion's documentation site: Sphinx with Furo and MyST, like the canned-schedules docs. Build with
# mise run docs:build.
project = "canned-schedules-companion"
html_title = "canned-schedules-companion"
html_theme = "furo"
html_baseurl = "https://iilei.github.io/canned-schedules-companion/"
html_theme_options = {
    "source_repository": "https://github.com/iilei/canned-schedules-companion/",
    "source_branch": "master",
    "source_directory": "docs/",
    # Served from _static (fonts.css), with the system fonts as fallback. Furo uses these in dark mode too.
    "light_css_variables": {
        "font-stack": 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif',
        "font-stack--headings": '"Source Serif 4", Georgia, "Times New Roman", serif',
        "font-stack--monospace": '"Noto Sans Mono", SFMono-Regular, Menlo, Consolas, monospace',
    },
}
html_static_path = ["_static"]
html_css_files = ["fonts.css"]

extensions = ["myst_parser"]
# Anchors for headings down to ###, named like GitHub's, so links such as publish.md#publish-prepare work.
myst_heading_anchors = 3
myst_enable_extensions = ["colon_fence"]

exclude_patterns = ["_build", "requirements.*"]
suppress_warnings = ["myst.header", "misc.highlighting_failure"]
