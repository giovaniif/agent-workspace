import { readFile } from "node:fs/promises";
import { chromium } from "playwright";

const publicDir = new URL("../public/", import.meta.url);
const macosDir = new URL("../../macos/", import.meta.url);

const icons = [
  { dir: publicDir, file: "icon-192.png", size: 192, bleed: false, scale: 1 },
  { dir: publicDir, file: "icon-512.png", size: 512, bleed: false, scale: 1 },
  { dir: publicDir, file: "icon-maskable-512.png", size: 512, bleed: true, scale: 1 },
  { dir: publicDir, file: "apple-touch-icon.png", size: 180, bleed: true, scale: 1 },
  { dir: macosDir, file: "AppIcon.png", size: 1024, bleed: false, scale: 0.8 },
];

function page(svg: string, size: number, bleed: boolean, scale: number): string {
  const scaled = scale === 1 ? svg : svg.replace("<svg ", `<svg style="transform: scale(${scale})" `);
  const art = bleed ? svg.replace('rx="112"', 'rx="0"').replace("<svg ", '<svg style="transform: scale(0.82)" ') : scaled;
  const background = bleed ? "#1e66f5" : "transparent";
  return `<!doctype html><html><body style="margin:0;width:${size}px;height:${size}px;background:${background};display:grid;place-items:center">${art.replace("<svg ", `<svg width="${size}" height="${size}" `)}</body></html>`;
}

const svg = await readFile(new URL("icon.svg", publicDir), "utf8");
const browser = await chromium.launch();
try {
  for (const icon of icons) {
    const tab = await browser.newPage({ viewport: { width: icon.size, height: icon.size } });
    await tab.setContent(page(svg, icon.size, icon.bleed, icon.scale));
    await tab.screenshot({ path: new URL(icon.file, icon.dir).pathname, omitBackground: !icon.bleed });
    await tab.close();
    console.log("wrote " + new URL(icon.file, icon.dir).pathname);
  }
} finally {
  await browser.close();
}
