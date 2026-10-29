import assert from 'node:assert/strict';
import { test } from 'node:test';
import { nextIndex, orderAfterAppend, orderAfterInsertNext, orderAfterRemove, shuffled } from '../src/queue.ts';

const isPermutation = (order: number[], length: number) =>
  order.length === length && new Set(order).size === length && order.every((i) => i >= 0 && i < length);

test('shuffled starts with the current song and covers the queue', () => {
  for (let n = 1; n < 30; n++) {
    const order = shuffled(n, n - 1);
    assert.equal(order[0], n - 1);
    assert.ok(isPermutation(order, n));
  }
  assert.deepEqual(shuffled(0, 0), []);
});

test('play next in shuffle plays the inserted song next and keeps the rest', () => {
  // Queue 0..4, playing 2, shuffle order 3 → 2 → 0 → 4 → 1 (3 already played).
  const order = [3, 2, 0, 4, 1];
  const after = orderAfterInsertNext(order, 5, 2)!;
  // The new song sits at index 3; old 3 and 4 move to 4 and 5.
  assert.deepEqual(after, [4, 2, 3, 0, 5, 1]);
  assert.equal(nextIndex(6, 2, true, after, true), 3);
  assert.ok(isPermutation(after, 6));
});

test('adding to the queue in shuffle plays it last', () => {
  const after = orderAfterAppend([1, 0, 2], 3)!;
  assert.deepEqual(after, [1, 0, 2, 3]);
  assert.equal(nextIndex(4, 2, true, after, false), 3);
});

test('adding twice in a row keeps both songs', () => {
  let order = [1, 0, 2];
  order = orderAfterAppend(order, 3)!;
  order = orderAfterAppend(order, 4)!;
  assert.ok(isPermutation(order, 5));
});

test('removing a song keeps the remaining shuffle order', () => {
  assert.deepEqual(orderAfterRemove([3, 2, 0, 4, 1], 5, 0), [2, 1, 3, 0]);
  assert.ok(isPermutation(orderAfterRemove([3, 2, 0, 4, 1], 5, 4)!, 4));
});

test('a stale order is ignored rather than corrupted', () => {
  assert.equal(orderAfterInsertNext([0, 1], 3, 0), null);
  assert.equal(orderAfterAppend([0, 1], 3), null);
  assert.equal(orderAfterRemove([0, 1], 3, 1), null);
  assert.equal(nextIndex(3, 1, true, [0, 1], true), 2);
});
