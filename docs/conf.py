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
}

extensions = ["myst_parser"]
# Anchors for headings down to ###, named like GitHub's, so links such as publish.md#publish-prepare work.
myst_heading_anchors = 3
myst_enable_extensions = ["colon_fence"]

exclude_patterns = ["_build", "requirements.*"]
suppress_warnings = ["myst.header", "misc.highlighting_failure"]
