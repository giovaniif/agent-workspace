import { readFile } from "node:fs/promises";
import { chromium } from "playwright";

const publicDir = new URL("../public/", import.meta.url);

const icons = [
  { file: "icon-192.png", size: 192, bleed: false },
  { file: "icon-512.png", size: 512, bleed: false },
  { file: "icon-maskable-512.png", size: 512, bleed: true },
  { file: "apple-touch-icon.png", size: 180, bleed: true },
];

function page(svg: string, size: number, bleed: boolean): string {
  const art = bleed ? svg.replace('rx="112"', 'rx="0"').replace("<svg ", '<svg style="transform: scale(0.82)" ') : svg;
  const background = bleed ? "#1e66f5" : "transparent";
  return `<!doctype html><html><body style="margin:0;width:${size}px;height:${size}px;background:${background};display:grid;place-items:center">${art.replace("<svg ", `<svg width="${size}" height="${size}" `)}</body></html>`;
}

const svg = await readFile(new URL("icon.svg", publicDir), "utf8");
const browser = await chromium.launch();
try {
  for (const icon of icons) {
    const tab = await browser.newPage({ viewport: { width: icon.size, height: icon.size } });
    await tab.setContent(page(svg, icon.size, icon.bleed));
    await tab.screenshot({ path: new URL(icon.file, publicDir).pathname, omitBackground: !icon.bleed });
    await tab.close();
    console.log("wrote public/" + icon.file);
  }
} finally {
  await browser.close();
}
