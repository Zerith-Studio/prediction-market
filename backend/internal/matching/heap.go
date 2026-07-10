package matching

// orderHeap is a container/heap-backed priority queue of *RestingOrder, ordered by
// price-time priority: best price first, ties broken by earlier Seq (FIFO).
type orderHeap struct {
	orders *[]*RestingOrder
	// bestFirst: true = max-price-first (bids), false = min-price-first (asks)
	bestFirst bool
}

func newOrderHeap(bestFirst bool) *orderHeap {
	s := make([]*RestingOrder, 0)
	return &orderHeap{orders: &s, bestFirst: bestFirst}
}

func (h *orderHeap) Len() int { return len(*h.orders) }

func (h *orderHeap) Less(i, j int) bool {
	oi, oj := (*h.orders)[i], (*h.orders)[j]
	if oi.Order.Price != oj.Order.Price {
		if h.bestFirst {
			return oi.Order.Price > oj.Order.Price
		}
		return oi.Order.Price < oj.Order.Price
	}
	return oi.Seq < oj.Seq // earlier order wins ties
}

func (h *orderHeap) Swap(i, j int) {
	(*h.orders)[i], (*h.orders)[j] = (*h.orders)[j], (*h.orders)[i]
}

func (h *orderHeap) Push(x any) {
	*h.orders = append(*h.orders, x.(*RestingOrder))
}

func (h *orderHeap) Pop() any {
	old := *h.orders
	n := len(old)
	item := old[n-1]
	*h.orders = old[:n-1]
	return item
}
