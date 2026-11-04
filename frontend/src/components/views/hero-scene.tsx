'use client';
/* The WebGL part of the home hero: a faceted hub with the five sections orbiting it, tilting after the finger
   or the mouse and spun by dragging. three.js loads only here, after the page is up; without WebGL, or with
   prefers-reduced-motion, the CSS cube inside stays instead. Rendering stops off screen and in a hidden tab. */
import { useEffect, useRef, useState, type ReactNode } from 'react';

const css = (name: string, fb: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fb;

export function HeroScene({ fallback }: { fallback: ReactNode }) {
  const box = useRef<HTMLDivElement>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    const el = box.current;
    if (!el || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    let stop = () => {};
    let gone = false;
    import('three').then(T => {
      if (gone) return;
      const canvas = document.createElement('canvas');
      let renderer: InstanceType<typeof T.WebGLRenderer>;
      try { renderer = new T.WebGLRenderer({ canvas, antialias: true, alpha: true, powerPreference: 'low-power' }); }
      catch { return; } // no WebGL: the CSS cube stays
      renderer.setPixelRatio(Math.min(devicePixelRatio || 1, 2));
      el.appendChild(canvas);

      const scene = new T.Scene();
      const camera = new T.PerspectiveCamera(38, 1, 0.1, 50);
      camera.position.set(0, 0.7, 9.6);
      scene.add(new T.HemisphereLight(0xffffff, 0x334477, 1.6));
      const sun = new T.DirectionalLight(0xffffff, 2.2);
      sun.position.set(3, 5, 4);
      scene.add(sun);

      const world = new T.Group();
      scene.add(world);
      const mat = (c: string) => new T.MeshStandardMaterial({ color: new T.Color(c), flatShading: true, roughness: .55, metalness: .05 });

      // the hub: a faceted ball with a ring around it
      const hub = new T.Mesh(new T.IcosahedronGeometry(1.05, 0), mat('#FFFFFF'));
      world.add(hub);
      const ring = new T.Mesh(new T.TorusGeometry(1.65, 0.035, 8, 96), new T.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: .45 }));
      ring.rotation.x = Math.PI / 2.3;
      world.add(ring);

      // the five sections, each a shape of its own colour on its own tilted orbit
      const colours = [css('--c0', '#6A52D4'), css('--c1', '#C6457E'), css('--c2', '#1D8A66'), css('--c3', '#C2652A'), css('--c4', '#2F79BF')];
      const shapes = [
        new T.BoxGeometry(.5, .5, .5), new T.TorusGeometry(.26, .11, 10, 24), new T.ConeGeometry(.3, .55, 6),
        new T.OctahedronGeometry(.34, 0), new T.DodecahedronGeometry(.3, 0),
      ];
      const sats = shapes.map((g, i) => {
        const m = new T.Mesh(g, mat(colours[i]));
        const orbit = new T.Group();
        orbit.rotation.set(0.5 - i * 0.22, i * 1.26, 0.25 * (i % 2 ? 1 : -1));
        orbit.add(m);
        world.add(orbit);
        return { m, orbit, r: 1.95 + (i % 2) * 0.35, speed: 0.35 + i * 0.07, phase: i * 1.26 };
      });

      // a few specks of dust for depth
      const dots = new Float32Array(90 * 3);
      for (let i = 0; i < dots.length; i++) dots[i] = (Math.random() - .5) * 7;
      const dust = new T.Points(new T.BufferGeometry().setAttribute('position', new T.BufferAttribute(dots, 3)),
        new T.PointsMaterial({ color: 0xffffff, size: .045, transparent: true, opacity: .7 }));
      scene.add(dust);

      const size = () => {
        const w = el.clientWidth || 1, h = el.clientHeight || 1;
        renderer.setSize(w, h, false);
        camera.aspect = w / h;
        camera.updateProjectionMatrix();
      };
      size();
      const ro = new ResizeObserver(size);
      ro.observe(el);

      // pointer: tilt toward it; drag: spin, which then slows down
      let tx = 0, ty = 0, spin = 0, lastX: number | null = null;
      const move = (e: PointerEvent) => {
        const r = el.getBoundingClientRect();
        tx = ((e.clientY - r.top) / r.height - .5) * .6;
        ty = ((e.clientX - r.left) / r.width - .5) * .8;
        if (lastX !== null) { spin += (e.clientX - lastX) * .004; lastX = e.clientX; }
      };
      const down = (e: PointerEvent) => { lastX = e.clientX; };
      const up = () => { lastX = null; };
      el.addEventListener('pointermove', move);
      el.addEventListener('pointerdown', down);
      window.addEventListener('pointerup', up);

      let visible = true, raf = 0, prev = performance.now(), t = 0;
      const io = new IntersectionObserver(([e]) => { visible = e.isIntersecting; if (visible) loop(); });
      io.observe(el);
      const onVis = () => { if (!document.hidden) loop(); };
      document.addEventListener('visibilitychange', onVis);
      function loop() {
        cancelAnimationFrame(raf);
        raf = requestAnimationFrame(frame);
      }
      function frame(now: number) {
        if (!visible || document.hidden) return;
        const dt = Math.min((now - prev) / 1000, .05);
        prev = now;
        t += dt;
        spin *= .95;
        world.rotation.y += dt * .25 + spin;
        world.rotation.x += (tx - world.rotation.x) * .06;
        world.rotation.z += (-ty * .3 - world.rotation.z) * .06;
        hub.rotation.x += dt * .4;
        hub.rotation.y += dt * .3;
        hub.position.y = Math.sin(t * 1.4) * .08;
        for (const s of sats) {
          const a = s.phase + t * s.speed;
          s.m.position.set(Math.cos(a) * s.r, Math.sin(a * 2) * .15, Math.sin(a) * s.r);
          s.m.rotation.x += dt * 1.1;
          s.m.rotation.y += dt * .8;
        }
        dust.rotation.y -= dt * .03;
        renderer.render(scene, camera);
        raf = requestAnimationFrame(frame);
      }
      prev = performance.now();
      loop();
      setReady(true);

      stop = () => {
        cancelAnimationFrame(raf);
        ro.disconnect();
        io.disconnect();
        document.removeEventListener('visibilitychange', onVis);
        el.removeEventListener('pointermove', move);
        el.removeEventListener('pointerdown', down);
        window.removeEventListener('pointerup', up);
        scene.traverse(o => {
          const m = o as InstanceType<typeof T.Mesh>;
          m.geometry?.dispose();
          const ms = Array.isArray(m.material) ? m.material : m.material ? [m.material] : [];
          ms.forEach(x => x.dispose());
        });
        renderer.dispose();
        canvas.remove();
      };
    }).catch(() => { /* the chunk did not load (offline): the CSS cube stays */ });
    return () => { gone = true; stop(); };
  }, []);

  return (
    <div className={`hero3d-scene${ready ? ' gl' : ''}`} ref={box} aria-hidden="true">
      {ready ? null : fallback}
    </div>
  );
}
