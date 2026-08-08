// 生成 switch-free 图标方案（多个版本）
// 运行: node icon-concepts/generate.js
// 需要 node-canvas
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

// ========== 方案 1：切换箭头 + 圆角方块（蓝紫渐变底，白色双箭头） ==========
function concept1() {
  const c = createCanvas(SIZE, SIZE);
  const ctx = c.getContext("2d");
  // 圆角方块底
  ctx.beginPath();
  ctx.roundRect(0, 0, SIZE, SIZE, 220);
  const grad = ctx.createLinearGradient(0, 0, SIZE, SIZE);
  grad.addColorStop(0, "#4F8CFF");
  grad.addColorStop(1, "#7C5CFF");
  ctx.fillStyle = grad;
  ctx.fill();
  // 白色双箭头（左右切换）
  ctx.strokeStyle = "#fff";
  ctx.lineCap = "round";
  ctx.lineWidth = 90;
  ctx.beginPath();
  ctx.moveTo(200, 512 - 150);
  ctx.lineTo(390, 512 - 150);
  ctx.lineTo(330, 512 - 250); // 上箭头
  ctx.moveTo(200, 512 + 150);
  ctx.lineTo(390, 512 + 150);
  ctx.lineTo(330, 512 + 250); // 下箭头
  // 右箭头
  ctx.moveTo(824, 512 - 150);
  ctx.lineTo(634, 512 - 150);
  ctx.lineTo(694, 512 - 250);
  ctx.moveTo(824, 512 + 150);
  ctx.lineTo(634, 512 + 150);
  ctx.lineTo(694, 512 + 250);
  ctx.stroke();
  save(c, "concept-1-switch-arrows");
}

// ========== 方案 2：闪电（Free 快速） + 切换环 ==========
function concept2() {
  const c = createCanvas(SIZE, SIZE);
  const ctx = c.getContext("2d");
  // 圆形底
  const grad = ctx.createRadialGradient(512, 512, 100, 512, 512, 512);
  grad.addColorStop(0, "#2D3BFF");
  grad.addColorStop(1, "#0A0E2E");
  ctx.fillStyle = grad;
  ctx.beginPath();
  ctx.arc(512, 512, 512, 0, Math.PI * 2);
  ctx.fill();
  // 黄色闪电
  ctx.fillStyle = "#FFD60A";
  ctx.beginPath();
  ctx.moveTo(600, 190);
  ctx.lineTo(340, 560);
  ctx.lineTo(520, 560);
  ctx.lineTo(420, 834);
  ctx.lineTo(700, 450);
  ctx.lineTo(530, 450);
  ctx.closePath();
  ctx.fill();
  save(c, "concept-2-lightning");
}

// ========== 方案 3：中转节点（Free 自由切换） ==========
function concept3() {
  const c = createCanvas(SIZE, SIZE);
  const ctx = c.getContext("2d");
  // 圆角方块深色底
  ctx.beginPath();
  ctx.roundRect(0, 0, SIZE, SIZE, 220);
  ctx.fillStyle = "#101426";
  ctx.fill();
  // 中央节点（蓝）
  const cg = ctx.createRadialGradient(512, 512, 0, 512, 512, 190);
  cg.addColorStop(0, "#5B9BFF");
  cg.addColorStop(1, "#1E5AD9");
  ctx.fillStyle = cg;
  ctx.beginPath();
  ctx.arc(512, 512, 180, 0, Math.PI * 2);
  ctx.fill();
  // 三条连接线（到三点）
  const pts = [
    [512, 512, 220, 512, 130],   // 上
    [512, 512, 512, 320, 512],   // 右
    [512, 512, 512, 512, 780],   // 下
  ];
  ctx.strokeStyle = "#8AB4FF";
  ctx.lineWidth = 26;
  ctx.lineCap = "round";
  for (const [x1, y1, x2, y2] of pts) {
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();
  }
  // 三个端点小圆
  ctx.fillStyle = "#FFD60A";
  for (const [, , x, y] of pts) {
    ctx.beginPath();
    ctx.arc(x, y, 36, 0, Math.PI * 2);
    ctx.fill();
  }
  save(c, "concept-3-hub");
}

// ========== 方案 4：首字母 S + 切换弧 ==========
function concept4() {
  const c = createCanvas(SIZE, SIZE);
  const ctx = c.getContext("2d");
  // 渐变圆角方块
  ctx.beginPath();
  ctx.roundRect(0, 0, SIZE, SIZE, 220);
  const g = ctx.createLinearGradient(0, 0, SIZE, SIZE);
  g.addColorStop(0, "#00C6FF");
  g.addColorStop(1, "#0072FF");
  ctx.fillStyle = g;
  ctx.fill();
  // 白色 S
  ctx.fillStyle = "#fff";
  ctx.font = "bold 620px Arial";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillText("S", 512, 545);
  // 底部切换弧
  ctx.strokeStyle = "#FFD60A";
  ctx.lineWidth = 36;
  ctx.lineCap = "round";
  ctx.beginPath();
  ctx.arc(512, 700, 200, Math.PI * 0.15, Math.PI * 0.85);
  ctx.stroke();
  save(c, "concept-4-S-swap");
}

// ========== 方案 5：双环切换（左右环对调，表达 switch） ==========
function concept5() {
  const c = createCanvas(SIZE, SIZE);
  const ctx = c.getContext("2d");
  ctx.fillStyle = "#0D1117";
  ctx.fillRect(0, 0, SIZE, SIZE);
  // 左环（蓝）
  ctx.strokeStyle = "#3D8BFF";
  ctx.lineWidth = 70;
  ctx.beginPath();
  ctx.arc(390, 512, 230, 0, Math.PI * 2);
  ctx.stroke();
  // 右环（紫）
  ctx.strokeStyle = "#9A5CFF";
  ctx.beginPath();
  ctx.arc(634, 512, 230, 0, Math.PI * 2);
  ctx.stroke();
  // 中间交换箭头
  ctx.strokeStyle = "#FFD60A";
  ctx.lineWidth = 44;
  ctx.lineCap = "round";
  // 上箭头向右
  ctx.beginPath();
  ctx.moveTo(340, 512 - 340);
  ctx.lineTo(684, 512 - 340);
  ctx.lineTo(620, 512 - 440);
  ctx.stroke();
  // 下箭头向左
  ctx.beginPath();
  ctx.moveTo(684, 512 + 340);
  ctx.lineTo(340, 512 + 340);
  ctx.lineTo(404, 512 + 440);
  ctx.stroke();
  save(c, "concept-5-rings");
}

concept1();
concept2();
concept3();
concept4();
concept5();
console.log("完成，共 5 个方案");
