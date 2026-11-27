// Hero scene: the Gateposts mark as a gate. Twelve review comments come in and pass through the gap one at a time.
// Eleven have no proof: they turn grey and are stacked and counted. One fails twice on the PR and passes on the fix:
// it turns red and hangs on the gate, and the gate locks. Illustration; the copy is the lab's (js/lab.js).
// Every pose is a pure function of the scene time t, so replay, pause and reduced motion need no extra state.
import * as THREE from 'three';
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js';
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js';

const COMMENTS = [
  ['Copilot', 'Consider a clearer name for r.'],
  ['Macroscope', 'This call may return a nil client on the error path.'],
  ['Bugbot', 'Possible race on the retry counter.'],
  ['CodeRabbit', 'A refund runs twice when the retry flag is set, so the customer is paid twice.'],
  ['CodeRabbit', 'Missing error check on Close().'],
  ['Macroscope', 'This log line may expose the refund amount.'],
  ['Copilot', 'Extract 3 into a named constant.'],
  ['Bugbot', 'The timeout value looks low for this call.'],
  ['CodeRabbit', 'Prefer errors.Is here.'],
  ['Macroscope', 'The parameter ctx is unused.'],
  ['Copilot', 'Add a doc comment to this exported function.'],
  ['Bugbot', 'This loop could be simpler.'],
];
const PROVEN = 3;
const STEP = 0.62;          // seconds between cards
const APPROACH = 1.6;
const SCAN = 0.5;
const SETTLE = 1.0;
const END = 0.4 + (COMMENTS.length - 1) * STEP + APPROACH + SCAN + SETTLE;

const host = document.getElementById('scene');
const hero = host && host.closest('.hero');
const tip = document.getElementById('tip');
const reduce = matchMedia('(prefers-reduced-motion: reduce)');

// The hero scopes its own night tokens, so read them from the hero, not the root.
function css(name) { return getComputedStyle(hero || document.documentElement).getPropertyValue(name).trim(); }
const clamp01 = (x) => Math.min(1, Math.max(0, x));
const ease = (x) => { x = clamp01(x); return x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2; };
const lerp = (a, b, k) => a + (b - a) * k;

function wrapText(ctx, text, maxW) {
  const words = text.split(' ');
  const lines = [];
  let cur = '';
  for (const w of words) {
    const next = cur ? cur + ' ' + w : w;
    if (ctx.measureText(next).width > maxW && cur) { lines.push(cur); cur = w; } else cur = next;
  }
  if (cur) lines.push(cur);
  return lines;
}

function cardTexture(who, what, pal, proven) {
  const W = 640, H = 360;
  const c = document.createElement('canvas');
  c.width = W; c.height = H;
  const g = c.getContext('2d');
  g.fillStyle = pal.panel;
  g.fillRect(0, 0, W, H);
  g.strokeStyle = proven ? pal.red : pal.rule;
  g.lineWidth = proven ? 14 : 4;
  g.strokeRect(g.lineWidth / 2, g.lineWidth / 2, W - g.lineWidth, H - g.lineWidth);
  g.fillStyle = proven ? pal.red : pal.muted;
  g.font = '500 30px "IBM Plex Mono", monospace';
  g.fillText(proven ? 'F1 PROVEN: BLOCKS THE MERGE' : who, 40, 66);
  g.fillStyle = pal.ink;
  g.font = '600 40px Overpass, sans-serif';
  const lines = wrapText(g, what, W - 80).slice(0, 4);
  lines.forEach((l, i) => g.fillText(l, 40, 132 + i * 52));
  if (proven) {
    g.fillStyle = pal.muted;
    g.font = '500 20px "IBM Plex Mono", monospace';
    g.fillText('fails twice on the PR, passes on the fix', 40, H - 34);
  }
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.anisotropy = 4;
  return t;
}

async function main() {
  if (!host) return;
  let renderer;
  try {
    renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
  } catch (e) {
    document.documentElement.classList.add('no-webgl');
    return;
  }
  const DPR = Math.min(devicePixelRatio, 1.75);
  renderer.setPixelRatio(DPR);
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFSoftShadowMap;
  renderer.domElement.setAttribute('aria-hidden', 'true');
  host.prepend(renderer.domElement);

  // Card text needs the web fonts; do not wait longer than 1.5 s for them.
  await Promise.race([document.fonts.ready, new Promise((r) => setTimeout(r, 1500))]);

  const readPalette = () => ({
    ground: css('--ground'), ground2: css('--ground-2'), panel: css('--panel'), rule: css('--rule'),
    muted: css('--muted'), ink: css('--ink'), iron: css('--iron'), red: css('--red'),
    lampRed: css('--lamp-red'), lampAmber: css('--lamp-amber'), lampGreen: css('--lamp-green'),
  });
  let pal = readPalette();
  // Cards are paper slips under the lamp, the same in both schemes.
  const paper = { panel: '#e9ece8', rule: '#bfc7c1', muted: '#4b5651', ink: '#0d1110', red: '#c32e18' };

  const scene = new THREE.Scene();
  scene.background = new THREE.Color(pal.ground);
  scene.fog = new THREE.Fog(pal.ground, 15, 36);
  const camera = new THREE.PerspectiveCamera(30, 1, 0.1, 100);
  const camBase = new THREE.Vector3(2.6, 4.2, 19);
  const look = new THREE.Vector3(-0.6, 2.4, 0);
  const lookWide = look.clone();

  const hemi = new THREE.HemisphereLight(0x9fb0a8, pal.ground2, 0.55);
  scene.add(hemi);
  // Moonlight from behind and above: rim light on the posts, long shadows toward the viewer.
  const sun = new THREE.DirectionalLight(0xcfe0ff, 1.1);
  sun.position.set(-3, 12, -9);
  sun.castShadow = true;
  sun.shadow.mapSize.set(2048, 2048);
  Object.assign(sun.shadow.camera, { left: -9, right: 9, top: 9, bottom: -3, near: 1, far: 40 });
  sun.shadow.bias = -0.0004;
  scene.add(sun);

  // The floor draws only shadows, so the page's own ground shows through and there is no seam.
  const floorMat = new THREE.ShadowMaterial({ color: 0x000000, opacity: 0.55 });
  const floor = new THREE.Mesh(new THREE.PlaneGeometry(120, 120), floorMat);
  floor.rotation.x = -Math.PI / 2;
  floor.receiveShadow = true;
  scene.add(floor);

  // The gate: two posts, the Gateposts mark built in iron.
  const iron = new THREE.MeshStandardMaterial({ color: pal.iron, roughness: 0.55, metalness: 0.35 });
  const gate = new THREE.Group();
  const POST_X = 1.55;
  for (const sx of [-1, 1]) {
    const post = new THREE.Mesh(new THREE.BoxGeometry(0.62, 5.2, 0.62), iron);
    post.position.set(sx * POST_X, 2.6, 0);
    const cap = new THREE.Mesh(new THREE.BoxGeometry(0.82, 0.16, 0.82), iron);
    cap.position.set(sx * POST_X, 5.28, 0);
    const plinth = new THREE.Mesh(new THREE.BoxGeometry(1.05, 0.28, 1.05), iron);
    plinth.position.set(sx * POST_X, 0.14, 0);
    for (const m of [post, cap, plinth]) { m.castShadow = true; m.receiveShadow = true; gate.add(m); }
  }
  scene.add(gate);

  // Signal lamps on the front of each post.
  const lampMat = new THREE.MeshStandardMaterial({ color: 0x222222, emissive: new THREE.Color(pal.lampAmber), emissiveIntensity: 0 });
  for (const sx of [-1, 1]) {
    const lamp = new THREE.Mesh(new THREE.SphereGeometry(0.13, 24, 16), lampMat);
    lamp.position.set(sx * POST_X, 4.75, 0.33);
    gate.add(lamp);
  }
  const glow = new THREE.PointLight(pal.lampAmber, 0, 10, 1.4);
  glow.position.set(0, 4.3, 1.4);
  scene.add(glow);

  // The check, hung between the posts as the gate's sign.
  const s = 3.1 / 16;
  const pt = (lx, ly) => new THREE.Vector3((lx - 12) * s, 4.35 + (12.4 - ly) * s * 0.9, 0);
  const path = new THREE.CurvePath();
  path.add(new THREE.LineCurve3(pt(7.25, 12.75), pt(10.5, 16)));
  path.add(new THREE.LineCurve3(pt(10.5, 16), pt(16.75, 9)));
  const checkMat = new THREE.MeshStandardMaterial({ color: pal.iron, roughness: 0.5, metalness: 0.3, emissive: new THREE.Color(pal.lampGreen), emissiveIntensity: 0 });
  const check = new THREE.Mesh(new THREE.TubeGeometry(path, 32, 0.085, 10, false), checkMat);
  check.castShadow = true;
  scene.add(check);

  // Scan light in the gap.
  const scanMat = new THREE.MeshBasicMaterial({ color: pal.lampAmber, transparent: true, opacity: 0, depthWrite: false, side: THREE.DoubleSide });
  const scanPlane = new THREE.Mesh(new THREE.PlaneGeometry(2.48, 3.6), scanMat);
  scanPlane.position.set(0, 2.0, 0);
  scene.add(scanPlane);
  const beamMat = new THREE.MeshBasicMaterial({ color: pal.lampAmber, transparent: true, opacity: 0 });
  const beam = new THREE.Mesh(new THREE.BoxGeometry(2.48, 0.03, 0.03), beamMat);
  scene.add(beam);

  // ---- Light effects. Light is the proof: its color always says the gate's state. ----
  const radial = (inner, outer) => {
    const c = document.createElement('canvas'); c.width = c.height = 128;
    const g = c.getContext('2d');
    const gr = g.createRadialGradient(64, 64, 0, 64, 64, 64);
    gr.addColorStop(0, inner); gr.addColorStop(0.25, 'rgba(255,255,255,.35)'); gr.addColorStop(1, outer);
    g.fillStyle = gr; g.fillRect(0, 0, 128, 128);
    const t = new THREE.CanvasTexture(c); t.colorSpace = THREE.SRGBColorSpace; return t;
  };
  const haloTex = radial('rgba(255,255,255,1)', 'rgba(255,255,255,0)');
  const additive = { transparent: true, depthWrite: false, blending: THREE.AdditiveBlending };

  // A shaft of light falls from behind the gate, through the gap, onto the ground in front.
  const shaftMat = new THREE.ShaderMaterial({
    ...additive, side: THREE.DoubleSide,
    uniforms: { uColor: { value: new THREE.Color(pal.lampAmber) }, uI: { value: 0.6 }, uT: { value: 0 } },
    vertexShader: `varying vec2 vUv; varying vec3 vN; varying vec3 vV;
      void main(){ vUv = uv; vec4 mv = modelViewMatrix * vec4(position,1.0);
        vN = normalize(normalMatrix * normal); vV = normalize(-mv.xyz); gl_Position = projectionMatrix * mv; }`,
    fragmentShader: `uniform vec3 uColor; uniform float uI; uniform float uT; varying vec2 vUv; varying vec3 vN; varying vec3 vV;
      void main(){ float edge = pow(abs(dot(vN, vV)), 2.2);
        float fall = pow(vUv.y, 1.4) * smoothstep(0.0, 0.12, vUv.y);
        float flick = 0.92 + 0.08 * sin(uT * 1.7 + vUv.y * 9.0);
        float a = edge * fall * uI * flick; gl_FragColor = vec4(uColor * a, a); }`,
  });
  const shaft = new THREE.Mesh(new THREE.CylinderGeometry(0.35, 2.2, 10, 48, 1, true), shaftMat);
  shaft.position.set(0, 4.6, -1.2);
  shaft.rotation.x = -0.42;
  scene.add(shaft);

  // The pool of light where the shaft meets the ground.
  const poolMat = new THREE.MeshBasicMaterial({ map: haloTex, color: pal.lampAmber, ...additive, opacity: 0.55 });
  const pool = new THREE.Mesh(new THREE.PlaneGeometry(6.5, 4.5), poolMat);
  pool.rotation.x = -Math.PI / 2;
  pool.position.set(0, 0.02, 1.2);
  scene.add(pool);

  // A halo on each signal lamp, and one behind the proven card.
  const halos = [-1, 1].map((sx) => {
    const sp = new THREE.Sprite(new THREE.SpriteMaterial({ map: haloTex, color: pal.lampAmber, ...additive }));
    sp.position.set(sx * POST_X, 4.75, 0.45);
    sp.scale.setScalar(1.4);
    scene.add(sp);
    return sp;
  });
  const proofHalo = new THREE.Sprite(new THREE.SpriteMaterial({ map: haloTex, color: pal.lampRed, ...additive, opacity: 0 }));
  proofHalo.scale.set(4.2, 2.8, 1);
  scene.add(proofHalo);

  // Dust caught in the shaft.
  const DUST = 420;
  const dustPos = new Float32Array(DUST * 3);
  const dustSeed = new Float32Array(DUST);
  for (let i = 0; i < DUST; i++) {
    dustPos[i * 3] = (Math.random() - 0.5) * 3.2;
    dustPos[i * 3 + 1] = Math.random() * 8;
    dustPos[i * 3 + 2] = -2.5 + Math.random() * 4.5;
    dustSeed[i] = Math.random() * 6.28;
  }
  const dustGeo = new THREE.BufferGeometry();
  dustGeo.setAttribute('position', new THREE.BufferAttribute(dustPos, 3));
  const dotTex = radial('rgba(255,255,255,1)', 'rgba(255,255,255,0)');
  const dustMat = new THREE.PointsMaterial({ map: dotTex, color: pal.lampAmber, size: 0.07, sizeAttenuation: true, ...additive, opacity: 0.75 });
  const dust = new THREE.Points(dustGeo, dustMat);
  scene.add(dust);
  function drift(dt, t) {
    for (let i = 0; i < DUST; i++) {
      let y = dustPos[i * 3 + 1] + dt * (0.12 + (dustSeed[i] % 1) * 0.18);
      if (y > 8) y -= 8;
      dustPos[i * 3 + 1] = y;
      dustPos[i * 3] += Math.sin(t * 0.6 + dustSeed[i]) * dt * 0.05;
    }
    dustGeo.attributes.position.needsUpdate = true;
  }

  // A hand lamp: a warm light that follows the pointer across the front of the gate.
  const torch = new THREE.PointLight(0xffd9a0, 0, 9, 1.6);
  torch.position.set(0, 2.5, 3);
  scene.add(torch);
  const torchTarget = new THREE.Vector3(0, 2.5, 3);
  const torchPlane = new THREE.Plane(new THREE.Vector3(0, 0, 1), -2.6);

  // Cards.
  const cardGeo = new THREE.PlaneGeometry(1.7, 0.956);
  const cards = COMMENTS.map(([who, what], i) => {
    const mat = new THREE.MeshStandardMaterial({ map: cardTexture(who, what, paper, false), roughness: 0.85, side: THREE.DoubleSide, transparent: true });
    const m = new THREE.Mesh(cardGeo, mat);
    m.castShadow = true;
    m.userData = { i, who, what };
    const r = (k) => { const x = Math.sin((i + 1) * 12.9898 * k) * 43758.5453; return x - Math.floor(x); };
    // Cards rise out of the fog behind the gate and swing round the left post, clear of the page copy.
    m.userData.spawn = new THREE.Vector3(-1.8 + r(1) * 3.6, 1.2 + r(2) * 3.2, -16 + r(3) * 5);
    m.userData.ctrl = new THREE.Vector3(-3.0 - r(4) * 0.4, 3.0 + r(5) * 1.2, 1.2);
    m.userData.spin = (r(6) - 0.5) * 0.9;
    scene.add(m);
    return m;
  });
  let provenTex = cardTexture(COMMENTS[PROVEN][0], COMMENTS[PROVEN][1], paper, true);
  let normalTex = cards[PROVEN].material.map;
  const G0 = new THREE.Vector3(0, 2.0, 3.0);
  const G1 = new THREE.Vector3(0, 2.0, -0.2);
  const SIGN = new THREE.Vector3(0, 4.25, 0.35);
  const pileSlot = (k) => new THREE.Vector3(2.75 + (k % 2) * 0.2, 0.03 + k * 0.032, 2.3 + ((k * 37) % 5) * 0.05);

  const startOf = (i) => 0.4 + i * STEP;
  const unprovenRank = (i) => (i < PROVEN ? i : i - 1);
  const tmp = new THREE.Vector3();
  const grey = new THREE.Color(0x9aa29e);
  const white = new THREE.Color(0xffffff);

  function bez(a, c, b, k, out) {
    const u = 1 - k;
    return out.set(
      u * u * a.x + 2 * u * k * c.x + k * k * b.x,
      u * u * a.y + 2 * u * k * c.y + k * k * b.y,
      u * u * a.z + 2 * u * k * c.z + k * k * b.z,
    );
  }

  // Scene state for time t. Returns the HUD figures.
  function pose(t, idle) {
    let scanning = false, locked = false, entered = 0, hidden = 0;
    cards.forEach((m, i) => {
      const d = m.userData;
      const r = t - startOf(i);
      const mat = m.material;
      if (r < 0) { m.visible = false; return; }
      m.visible = true;
      entered++;
      if (r < APPROACH) {
        const k = ease(r / APPROACH);
        bez(d.spawn, d.ctrl, G0, k, m.position);
        m.rotation.set(0, lerp(-0.6 + d.spin, 0, k), lerp(d.spin * 0.4, 0, k));
        m.scale.setScalar(lerp(0.4, 1, clamp01(r / 0.4)));
        mat.color.copy(white); mat.opacity = clamp01(r / 0.3);
        mat.emissive.setRGB(0, 0, 0);
        return;
      }
      const rs = r - APPROACH;
      if (rs < SCAN) {
        const k = ease(rs / SCAN);
        m.position.lerpVectors(G0, G1, k);
        m.rotation.set(0, 0, 0);
        m.scale.setScalar(1);
        mat.color.copy(white); mat.opacity = 1;
        scanning = true;
        beam.position.set(0, lerp(0.3, 3.7, Math.abs(Math.sin(rs / SCAN * Math.PI * 1.5))), -0.2);
        return;
      }
      const rv = rs - SCAN;
      const k = ease(rv / SETTLE);
      if (i === PROVEN) {
        locked = true;
        if (mat.map !== provenTex) { mat.map = provenTex; mat.needsUpdate = true; }
        m.position.lerpVectors(G1, SIGN, k);
        if (idle) m.position.y += Math.sin(t * 1.2) * 0.04;
        m.rotation.set(0, idle ? Math.sin(t * 0.7) * 0.05 : 0, 0);
        m.scale.setScalar(lerp(1, 1.3, k));
        mat.color.copy(white); mat.opacity = 1;
        mat.emissive.set(pal.lampRed).multiplyScalar(0.22 * k);
      } else {
        hidden++;
        const slot = pileSlot(unprovenRank(i));
        const up = Math.sin(k * Math.PI) * 0.9;
        m.position.lerpVectors(G1, slot, k);
        m.position.y += up;
        m.rotation.set(lerp(0, -Math.PI / 2, k), 0, lerp(0, d.spin, k));
        m.scale.setScalar(lerp(1, 0.62, k));
        mat.color.copy(white).lerp(grey, clamp01(rv / 0.3));
        mat.opacity = lerp(1, 0.82, k);
        mat.emissive.setRGB(0, 0, 0);
      }
    });
    if (cards[PROVEN].material.map !== normalTex && t < startOf(PROVEN) + APPROACH + SCAN) {
      cards[PROVEN].material.map = normalTex; cards[PROVEN].material.needsUpdate = true;
    }

    // Lamps, scan light and the check.
    const lampColor = locked ? pal.lampRed : scanning ? pal.lampAmber : pal.lampGreen;
    lampMat.emissive.set(lampColor);
    glow.color.set(lampColor);
    const pulse = idle ? 0.85 + Math.sin(t * 2) * 0.15 : 1;
    lampMat.emissiveIntensity = (locked || scanning ? 4 : 1.6) * pulse;
    glow.intensity = (locked ? 14 : scanning ? 9 : 4) * pulse;
    scanMat.opacity = scanning ? 0.035 : 0;
    beamMat.opacity = scanning ? 1 : 0;
    beamMat.color.set(pal.lampAmber).multiplyScalar(3);

    // Light color follows the state: warm white at rest, amber while testing, red once a finding is proven.
    const lightColor = locked ? pal.lampRed : scanning ? pal.lampAmber : '#ffe7c2';
    shaftMat.uniforms.uColor.value.set(lightColor);
    shaftMat.uniforms.uI.value = (locked ? 0.95 : scanning ? 0.8 : 0.55) * pulse;
    shaftMat.uniforms.uT.value = t;
    poolMat.color.set(lightColor);
    poolMat.opacity = (locked ? 0.5 : 0.38) * pulse;
    dustMat.color.set(lightColor);
    halos.forEach((h) => { h.material.color.set(lampColor); h.material.opacity = (locked || scanning ? 0.9 : 0.45) * pulse; });
    const provenAt = startOf(PROVEN) + APPROACH + SCAN;
    proofHalo.material.opacity = clamp01((t - provenAt) / 0.8) * 0.55 * pulse;
    proofHalo.position.copy(cards[PROVEN].position).add(tmp.set(0, 0, -0.15));
    const signGone = clamp01((t - (startOf(PROVEN) + APPROACH + SCAN)) / 0.4);
    check.scale.setScalar(1 - signGone);
    check.visible = signGone < 1;
    checkMat.emissiveIntensity = locked || scanning ? 0 : 1.6;
    return { entered, hidden, scanning, locked };
  }

  // HUD
  const hudIn = document.querySelector('#hud-in b');
  const hudHidden = document.querySelector('#hud-hidden b');
  const hudGate = document.querySelector('#hud-gate b');
  const hudLamp = document.getElementById('hud-lamp');
  let lastHud = '';
  function hud(h) {
    const label = h.locked ? 'Merge locked: F1 proven' : h.scanning ? 'Testing' : h.entered ? 'Nothing proven yet' : 'Open';
    const key = h.entered + '|' + h.hidden + '|' + label;
    if (key === lastHud) return;
    lastHud = key;
    hudIn.textContent = h.entered;
    hudHidden.textContent = h.hidden;
    hudGate.textContent = label;
    hudLamp.className = 'lamp ' + (h.locked ? 'red' : h.scanning ? 'amber' : h.entered ? 'green' : '');
  }

  // Bloom: only bright things (lamps, the shaft, the scan line, the check) pass the threshold.
  const composer = new EffectComposer(renderer);
  composer.setPixelRatio(DPR);
  composer.addPass(new RenderPass(scene, camera));
  const bloom = new UnrealBloomPass(new THREE.Vector2(512, 512), 0.9, 0.6, 0.82);
  composer.addPass(bloom);
  composer.addPass(new OutputPass());

  // Size
  function resize() {
    const w = host.clientWidth, h = host.clientHeight;
    if (!w || !h) return;
    renderer.setSize(w, h, false);
    composer.setSize(w, h);
    camera.aspect = w / h;
    // Narrow hosts pull the camera back so the gate and the pile stay in frame.
    const back = w / h < 1 ? 1 + (1 - w / h) * 0.55 : 1;
    camBase.set(2.6 * back, 4.2 + (back - 1) * 1.5, 19 * back);
    // On a narrow host the gate sits in the middle, with no copy to its left.
    look.set(w < 700 ? 0.4 : lookWide.x, lookWide.y, lookWide.z);
    camera.updateProjectionMatrix();
    needsFrame = true;
  }

  // Pointer: a small parallax, and hover to read a card.
  const pointer = new THREE.Vector2(0, 0);
  const ndc = new THREE.Vector2(9, 9);
  const ray = new THREE.Raycaster();
  let hovered = null;
  hero.addEventListener('pointermove', (e) => {
    const r = hero.getBoundingClientRect();
    pointer.set((e.clientX - r.left) / r.width * 2 - 1, (e.clientY - r.top) / r.height * 2 - 1);
    const c = host.getBoundingClientRect();
    ndc.set((e.clientX - c.left) / c.width * 2 - 1, -((e.clientY - c.top) / c.height) * 2 + 1);
    tipAt = { x: e.clientX - r.left, y: e.clientY - r.top, w: r.width };
    if (Math.abs(ndc.x) <= 1.2 && Math.abs(ndc.y) <= 1.2) {
      ray.setFromCamera(ndc, camera);
      ray.ray.intersectPlane(torchPlane, torchTarget);
      torchOn = 1;
    }
    needsFrame = true;
  });
  hero.addEventListener('pointerleave', () => { ndc.set(9, 9); pointer.set(0, 0); torchOn = 0; needsFrame = true; });
  let tipAt = null;
  let torchOn = 0;

  function hoverCheck(t) {
    if (ndc.x > 1.5) { setTip(null); return; }
    ray.setFromCamera(ndc, camera);
    const hit = ray.intersectObjects(cards.filter((m) => m.visible), false)[0];
    setTip(hit ? hit.object : null, t);
  }
  function setTip(m, t) {
    if (m === hovered && m) { placeTip(); return; }
    hovered = m;
    document.body.style.cursor = m ? 'help' : '';
    if (!m) { tip.classList.remove('on'); return; }
    const d = m.userData;
    const decided = t >= startOf(d.i) + APPROACH + SCAN;
    const verdict = !decided ? 'Testing' : d.i === PROVEN ? 'Proven: fails twice on the PR, passes on the fix. Blocks the merge.' : 'Unproven: no test backs it. Hidden and counted.';
    tip.innerHTML = '';
    const b = document.createElement('b'); b.textContent = d.who;
    const p = document.createElement('span'); p.textContent = d.what;
    const v = document.createElement('span'); v.className = 'v' + (decided && d.i === PROVEN ? ' red' : ''); v.textContent = verdict;
    tip.append(b, p, v);
    tip.classList.add('on');
    placeTip();
  }
  function placeTip() {
    if (!tipAt) return;
    const x = Math.min(tipAt.x + 16, tipAt.w - 320);
    tip.style.left = x + 'px';
    tip.style.top = (tipAt.y + 18) + 'px';
  }

  // Clock
  let t = 0, last = performance.now(), paused = false, visible = true, needsFrame = true, raf = 0;
  const pauseBtn = document.getElementById('scene-pause');
  const replayBtn = document.getElementById('scene-replay');
  pauseBtn.addEventListener('click', () => {
    paused = !paused;
    pauseBtn.textContent = paused ? 'Play' : 'Pause';
    pauseBtn.setAttribute('aria-pressed', String(paused));
    last = performance.now();
    loop();
  });
  replayBtn.addEventListener('click', () => {
    t = reduce.matches ? END : 0;
    last = performance.now();
    lastHud = '';
    needsFrame = true;
    loop();
  });

  const camTarget = new THREE.Vector3();
  function frame(now) {
    raf = 0;
    const dt = Math.min(0.05, (now - last) / 1000);
    last = now;
    const moving = !paused && !reduce.matches;
    if (moving) t += dt;
    const idle = t >= END;
    const h = pose(t, idle && moving);
    hud(h);
    camTarget.set(camBase.x + pointer.x * 0.9, camBase.y - pointer.y * 0.5, camBase.z);
    if (reduce.matches) camera.position.copy(camBase);
    else camera.position.lerp(camTarget, 0.06);
    camera.lookAt(look);
    if (moving) drift(dt, t);
    torch.position.lerp(torchTarget, reduce.matches ? 1 : 0.12);
    torch.intensity = lerp(torch.intensity, torchOn * 7, reduce.matches ? 1 : 0.08);
    hoverCheck(t);
    composer.render();
    needsFrame = false;
    const settling = (camera.position.distanceToSquared(camTarget) > 1e-4 || Math.abs(torch.intensity - torchOn * 7) > 0.05) && !reduce.matches;
    if (visible && (moving || settling || needsFrame)) loop();
  }
  function loop() { if (!raf && visible) raf = requestAnimationFrame(frame); }

  new ResizeObserver(() => { resize(); loop(); }).observe(host);
  new IntersectionObserver((es) => {
    visible = es[0].isIntersecting && !document.hidden;
    if (visible) { last = performance.now(); loop(); }
  }, { threshold: 0.02 }).observe(host);
  document.addEventListener('visibilitychange', () => {
    visible = !document.hidden;
    if (visible) { last = performance.now(); loop(); }
  });
  hero.addEventListener('pointermove', loop);
  reduce.addEventListener('change', () => { if (reduce.matches) t = END; needsFrame = true; loop(); });

  // Follow the light or dark scheme.
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    pal = readPalette();
    scene.fog.color.set(pal.ground);
    scene.background.set(pal.ground);
    hemi.groundColor.set(pal.ground2);
    iron.color.set(pal.iron);
    checkMat.color.set(pal.iron);
    cards.forEach((m, i) => {
      const fresh = cardTexture(m.userData.who, m.userData.what, paper, false);
      if (i === PROVEN) {
        const wasProven = m.material.map === provenTex;
        normalTex.dispose(); provenTex.dispose();
        normalTex = fresh;
        provenTex = cardTexture(m.userData.who, m.userData.what, paper, true);
        m.material.map = wasProven ? provenTex : normalTex;
      } else {
        m.material.map.dispose();
        m.material.map = fresh;
      }
      m.material.needsUpdate = true;
    });
    needsFrame = true;
    loop();
  });

  if (reduce.matches) t = END;
  resize();
  camera.position.copy(camBase);
  loop();
  host.dataset.ready = 'true';
}

main().catch((e) => {
  document.documentElement.classList.add('no-webgl');
  console.error('gate scene', e);
});
