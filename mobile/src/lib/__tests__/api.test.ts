import { mapBook } from "../api";

test("mapBook unifies the outcome-indexed book into one YES ladder", () => {
  // Go book: [0]=NO, [1]=YES. YES bids = BUY YES ∪ complement(SELL NO);
  // YES asks = SELL YES ∪ complement(BUY NO).
  const book = mapBook({
    bids: [
      [{ price: 40, size: 10 }], // BUY NO 40 → YES ask at 60
      [{ price: 55, size: 5 }],  // BUY YES 55 → YES bid 55
    ],
    asks: [
      [{ price: 45, size: 7 }],  // SELL NO 45 → YES bid at 55 (merges with above)
      [{ price: 61, size: 3 }],  // SELL YES 61 → YES ask 61
    ],
  });
  expect(book.bids).toEqual([{ price: 55, size: 12 }]); // merged 5 + 7
  expect(book.asks).toEqual([
    { price: 60, size: 10 },
    { price: 61, size: 3 },
  ]); // ascending
});

test("mapBook tolerates missing sides", () => {
  const book = mapBook({ bids: [[], []], asks: [[], []] });
  expect(book).toEqual({ bids: [], asks: [] });
});
