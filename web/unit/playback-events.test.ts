import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createRecorder } from '../src/playback-events.ts';

test('intents are recorded with their source', () => {
  let t = 1000;
  const r = createRecorder(() => t);
  r.intent('pause', 'mediasession', 'playing');
  t += 8000;
  r.intent('play', 'mediasession', 'paused');
  assert.deepEqual(
    r.flush().map((e) => [e.action, e.from, e.state, e.at]),
    [
      ['pause', 'mediasession', 'playing', 1000],
      ['play', 'mediasession', 'paused', 9000],
    ],
  );
  assert.deepEqual(r.flush(), []);
});

test('an audio change right after an intent is its result, a stray one is logged', () => {
  let t = 0;
  const r = createRecorder(() => t);
  r.intent('pause', 'button', 'playing');
  t += 100;
  r.observed('pause', 'playing');
  t += 5000;
  r.observed('play', 'paused'); // nobody asked: the OS or WebView played it
  const got = r.flush();
  assert.equal(got.length, 2);
  assert.deepEqual([got[1].action, got[1].from], ['play', 'audio']);
});

test('the queue is bounded', () => {
  const r = createRecorder(() => 0, 3);
  for (let i = 0; i < 10; i++) r.intent('play', 'auto', String(i));
  assert.deepEqual(
    r.flush().map((e) => e.state),
    ['7', '8', '9'],
  );
});
