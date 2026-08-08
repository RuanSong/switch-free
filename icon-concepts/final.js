// 生成 switch-free 最终图标（方案 3 中转节点终稿）
// 运行: node icon-concepts/final.js
const fs = require("fs");
const path = require("path");
const { createCanvas } = require("canvas");

const OUT = path.join(__dirname);
const SIZE = 1024;

function save(canvas, name) {
  const buf = canvas.toBuffer("image/png");
  fs.writeFileSync(path.join(OUT, name + ".png"), buf);
  console.log("✓", name + ".png");
}

// ===== 终稿：中转节点（三色端点 = 三上游）=====
function final() {
  const c = createCanvas(SIZE, SIZE);
  const ctx = c.getContext("2d");

  // 圆角方块深色底（带细微渐变）
  ctx.beginPath();
  ctx.roundRect(0, 0, SIZE, SIZE, 200);
  const bg = ctx.createLinearGradient(0, 0, SIZE, SIZE);
  bg.addColorStop(0, "#0B1020");
  bg.addColorStop(1, "#131A33");
  ctx.fillStyle = bg;
  ctx.fill();

  // 三上游端点位置（上、右、左下，模拟连接三源）
  const pts = [
    { x: 512, y: 205, color: "#22C55E" },   // 上 - JoyCode 绿
    { x: 785, y: 640, color: "#F59E0B" },   // 右下 - DevEco 橙
    { x: 239, y: 640, color: "#A855F7" },   // 左下 - OpenCode 紫
  ];

  // 连接线（从中心节点到三点）
  ctx.strokeStyle = "#4B6BFF";
  ctx.lineWidth = 30;
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  for (const p of pts) {
    ctx.beginPath();
    ctx.moveTo(512, 512);
    ctx.lineTo(p.x, p.y);
    ctx.stroke();
  }

  // 中央节点（蓝，发光感）
  const cg = ctx.createRadialGradient(512, 512, 0, 512, 512, 195);
  cg.addColorStop(0, "#6EA8FF");
  cg.addColorStop(0.7, "#2E6BFF");
  cg.addColorStop(1, "#1B4FD8");
  ctx.fillStyle = cg;
  ctx.beginPath();
  ctx.arc(512, 512, 190, 0, Math.PI * 2);
  ctx.fill();
  // 节点高光
  ctx.fillStyle = "rgba(255,255,255,0.25)";
  ctx.beginPath();
  ctx.arc(462, 462, 55, 0, Math.PI * 2);
  ctx.fill();

  // 三个端点（带白色描边增强对比）
  for (const p of pts) {
    ctx.strokeStyle = "rgba(255,255,255,0.35)";
    ctx.lineWidth = 8;
    ctx.fillStyle = p.color;
    ctx.beginPath();
    ctx.arc(p.x, p.y, 58, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
  }

  save(c, "switch-free-icon-final");
}

final();
console.log("终稿生成完成");
