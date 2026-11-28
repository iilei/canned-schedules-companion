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
        # The playground's accents (canned-schedules web/style.css): teal for links and navigation, violet for
        # visited links, the hover underline, the current page and "important".
        "color-brand-primary": "#0b7a68",
        "color-brand-content": "#0b7a68",
        "color-brand-visited": "#8250df",
        "color-link-underline--hover": "#8250df",
        "color-toc-item-text--active": "#8250df",
        "color-highlighted-background": "#e2f4ef",
        "color-admonition-title--important": "#8250df",
        "color-admonition-title-background--important": "rgba(130, 80, 223, 0.1)",
        "color-accent-current": "#8250df",
    },
    "dark_css_variables": {
        "color-brand-primary": "#5ad1b8",
        "color-brand-content": "#5ad1b8",
        "color-brand-visited": "#c4a5ff",
        "color-link-underline--hover": "#c4a5ff",
        "color-toc-item-text--active": "#c4a5ff",
        "color-highlighted-background": "#15302b",
        "color-admonition-title--important": "#c4a5ff",
        "color-admonition-title-background--important": "rgba(196, 165, 255, 0.12)",
        "color-accent-current": "#c4a5ff",
    },
}
html_static_path = ["_static"]
templates_path = ["_templates"]  # base.html: font preloads
html_css_files = ["fonts.css", "theme.css"]
# Both from the a11y syntax highlighting project (Furo's default dark style, native, is unrelated to its light one).
pygments_style = "a11y-light"
pygments_dark_style = "a11y-dark"

extensions = ["myst_parser"]
# Anchors for headings down to ###, named like GitHub's, so links such as publish.md#publish-prepare work.
myst_heading_anchors = 3
myst_enable_extensions = ["colon_fence"]

exclude_patterns = ["_build", "requirements.*"]
suppress_warnings = ["myst.header", "misc.highlighting_failure"]
