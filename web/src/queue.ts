// Play-queue arithmetic, kept free of React so it can be tested on its own.
// `order` is the shuffled play order: a permutation of queue indexes whose
// first entries are the songs already played. It is only meaningful while
// it has exactly one entry per queue item.

export function shuffled(length: number, first: number): number[] {
  const rest = Array.from({ length }, (_, i) => i).filter((i) => i !== first);
  for (let i = rest.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [rest[i], rest[j]] = [rest[j], rest[i]];
  }
  return length > 0 ? [first, ...rest] : [];
}

/** The index after the current one, honouring shuffle. */
export function nextIndex(length: number, index: number, shuffle: boolean, order: number[], wrap: boolean): number | null {
  if (length === 0) return null;
  if (shuffle && order.length === length) {
    const position = order.indexOf(index);
    if (position + 1 < order.length) return order[position + 1];
    return wrap ? order[0] : null;
  }
  if (index + 1 < length) return index + 1;
  return wrap ? 0 : null;
}

/** A song inserted right after the current one also plays right after it
 * in shuffle order; everything else keeps its place. */
export function orderAfterInsertNext(order: number[], length: number, current: number): number[] | null {
  if (order.length !== length) return null;
  const at = current + 1;
  const shifted = order.map((i) => (i >= at ? i + 1 : i));
  const position = shifted.indexOf(current);
  shifted.splice(position + 1, 0, at);
  return shifted;
}

/** A song appended to the queue plays last in shuffle order too. */
export function orderAfterAppend(order: number[], length: number): number[] | null {
  if (order.length !== length) return null;
  return [...order, length];
}

/** Removing one song keeps the remaining shuffle order. */
export function orderAfterRemove(order: number[], length: number, removed: number): number[] | null {
  if (order.length !== length) return null;
  return order.filter((i) => i !== removed).map((i) => (i > removed ? i - 1 : i));
}
