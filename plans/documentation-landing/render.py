"""Render a frozen HTML design with Playwright 1.59.0 and a Chromium executable."""
import argparse
import tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright

root = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser()
parser.add_argument("round", type=Path)
parser.add_argument("--chromium", required=True)
args = parser.parse_args()
folder = args.round.resolve()
with tempfile.TemporaryDirectory() as work, sync_playwright() as playwright:
    directory = Path(work)
    template = (folder / "landing.html").read_text()
    symbols = (folder / "symbols.svg").read_text()
    for token, path in {"HOME_IMAGE": "docs/content/assets/desktop-home.png",
                        "QUEUE_IMAGE": "docs/content/assets/desktop-queue.png",
                        "PROJECT_IMAGE": "docs/content/assets/desktop-project.png",
                        "CONFLICT_DETAIL": "plans/documentation-landing/home-conflict-detail.png"}.items():
        template = template.replace(token, (root / path).as_uri())
    (directory / "index.html").write_text(template.replace("<!-- symbols -->", symbols))
    font = root / "docs/site/src/assets/dm-sans-latin-wght-normal.woff2"
    (directory / "landing.css").write_text((folder / "landing.css").read_text().replace("FONT_DM", font.as_uri()))
    browser = playwright.chromium.launch(executable_path=args.chromium)
    print("Chromium", browser.version)
    for name, width, height in [("desktop", 1440, 1000), ("mobile", 390, 844)]:
        page = browser.new_page(viewport={"width": width, "height": height}, device_scale_factor=1)
        page.goto((directory / "index.html").as_uri())
        page.evaluate("document.fonts.ready")
        assert page.locator("img").evaluate_all("imgs => imgs.every(i => i.complete && i.naturalWidth > 0)")
        assert page.evaluate("document.documentElement.scrollWidth === innerWidth")
        page.screenshot(path=folder / (name + ".png"), full_page=True)
        if page.locator(".explorer").count():
            zoom = page.locator(".window:visible .zoom-control input")
            if zoom.count():
                zoom.check()
                page.mouse.move(0, 0)
                page.locator(".explorer").screenshot(path=folder / (name + "-zoom.png"))
                zoom.uncheck()
            for detail in ("project", "device"):
                page.locator(f'input[value="{detail}"]').check()
                if zoom.count():
                    page.mouse.move(0, 0)
                page.locator(".explorer").screenshot(path=folder / (name + "-" + detail + ".png"))
        page.close()
    browser.close()
