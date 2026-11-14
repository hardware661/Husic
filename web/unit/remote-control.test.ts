import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pickPlaylist, parseCommand } from '../src/remote-control.ts';

const lists = [
  { id: 1, name: '我喜欢的音乐' },
  { id: 7, name: '早上听' },
  { id: 8, name: '开车' },
];

test('commands are parsed strictly', () => {
  assert.deepEqual(parseCommand('{"action":"play","what":"liked"}'), { action: 'play', what: 'liked' });
  assert.deepEqual(parseCommand('{"action":"next"}'), { action: 'next', what: '' });
  assert.equal(parseCommand('{"action":"rm"}'), null);
  assert.equal(parseCommand('not json'), null);
});

test('a playlist is found by the closest name', () => {
  assert.equal(pickPlaylist(lists, '开车')?.id, 8);
  assert.equal(pickPlaylist(lists, '早上')?.id, 7);
  assert.equal(pickPlaylist(lists, '睡觉'), null);
});
