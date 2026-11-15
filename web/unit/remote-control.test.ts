import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createSessionGate, pickPlaylist, parseCommand, sameState } from '../src/remote-control.ts';

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

test('sameState only changes on song or play state', () => {
  const a = { title: '晴天', artist: '周杰伦', playing: true };
  assert.equal(sameState(null, a), false);
  assert.equal(sameState(a, { ...a }), true);
  assert.equal(sameState(a, { ...a, playing: false }), false);
  assert.equal(sameState(a, { ...a, title: '七里香' }), false);
});

test('a Bluetooth play only resumes what Xiao Ai interrupted, not what the owner paused', () => {
  const gate = createSessionGate();
  assert.equal(gate.allowPlay(), true); // Xiao Ai talked over the music: it comes back
  gate.ownerPaused(); // "暂停" to the speaker, the pause button, a media key
  assert.equal(gate.allowPlay(), false); // Xiao Ai answering "没事了" or an alarm later (11-15)
  gate.ownerPlayed(); // played again in Husic, the speaker panel or with a key
  assert.equal(gate.allowPlay(), true);
});

test('off outside the desktop app, a session play always goes through', () => {
  const gate = createSessionGate(false);
  gate.ownerPaused();
  assert.equal(gate.allowPlay(), true);
});
