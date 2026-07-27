const puppeteer = require('puppeteer');
const fs = require('fs');

async function capture() {
  console.log("Launching headless browser...");
  const browser = await puppeteer.launch({ headless: 'new' });
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080 });

  console.log("Capturing Dashboard Fleet Map (Halt State)...");
  try {
    await page.goto('http://localhost:3000/dashboard/fleet');
    await page.waitForTimeout(2000);
    await page.screenshot({ path: 'docs/screenshots/fleet_map_halt.png' });
  } catch (e) { console.log("Skipping (service down)"); }

  console.log("Capturing Dashboard Budgets...");
  try {
    await page.goto('http://localhost:3000/dashboard/budgets');
    await page.waitForTimeout(2000);
    await page.screenshot({ path: 'docs/screenshots/budgets_headroom.png' });
  } catch (e) { console.log("Skipping (service down)"); }

  console.log("Capturing Grafana Enforcement Dashboard...");
  try {
    await page.goto('http://localhost:3000/d/containment/containment-overrides?orgId=1');
    await page.waitForTimeout(3000);
    await page.screenshot({ path: 'docs/screenshots/grafana_enforcement.png' });
  } catch (e) { console.log("Skipping (service down)"); }

  console.log("Capturing Grafana Fleet Risk Dashboard...");
  try {
    await page.goto('http://localhost:3000/d/budget/budget-velocity-exceptions?orgId=1');
    await page.waitForTimeout(3000);
    await page.screenshot({ path: 'docs/screenshots/grafana_fleet_risk.png' });
  } catch (e) { console.log("Skipping (service down)"); }

  await browser.close();
  console.log("Screenshots saved to docs/screenshots/");
}

capture();
