import WidgetKit
import SwiftUI

// MARK: - Timeline plumbing (shared shape for both widgets)

struct PortfolioEntry: TimelineEntry {
    let date: Date
    let state: LoadState<PortfolioSummary>
}

struct ActiveMarketEntry: TimelineEntry {
    let date: Date
    let state: LoadState<ActiveMarketData>
}

private func schedule(_ ok: Bool) -> Date {
    // 15 min on success, 5 min after a failure, per the design spec.
    Date().addingTimeInterval(ok ? 15 * 60 : 5 * 60)
}

struct PortfolioProvider: TimelineProvider {
    func placeholder(in _: Context) -> PortfolioEntry {
        PortfolioEntry(date: .now, state: .data(PortfolioSummary(
            availableMicro: 1_000_000_000, valueMicro: 250_000_000,
            unrealizedMicro: 12_500_000, realizedMicro: 4_200_000,
            lockedMicro: 50_000_000, openOrders: 3, positions: [], orders: [])))
    }
    func getSnapshot(in _: Context, completion: @escaping (PortfolioEntry) -> Void) {
        Task { completion(PortfolioEntry(date: .now, state: await WidgetLoader.loadPortfolio())) }
    }
    func getTimeline(in _: Context, completion: @escaping (Timeline<PortfolioEntry>) -> Void) {
        Task {
            let state = await WidgetLoader.loadPortfolio()
            let ok = if case .failure = state { false } else { true }
            completion(Timeline(
                entries: [PortfolioEntry(date: .now, state: state)],
                policy: .after(schedule(ok))))
        }
    }
}

struct ActiveMarketProvider: TimelineProvider {
    func placeholder(in _: Context) -> ActiveMarketEntry {
        ActiveMarketEntry(date: .now, state: .noWallet)
    }
    func getSnapshot(in _: Context, completion: @escaping (ActiveMarketEntry) -> Void) {
        Task { completion(ActiveMarketEntry(date: .now, state: await WidgetLoader.loadActiveMarket())) }
    }
    func getTimeline(in _: Context, completion: @escaping (Timeline<ActiveMarketEntry>) -> Void) {
        Task {
            let state = await WidgetLoader.loadActiveMarket()
            let ok = if case .failure = state { false } else { true }
            completion(Timeline(
                entries: [ActiveMarketEntry(date: .now, state: state)],
                policy: .after(schedule(ok))))
        }
    }
}

// MARK: - Shared view bits

struct Eyebrow: View {
    let text: String
    var body: some View {
        Text(text.uppercased())
            .font(.system(size: 9, weight: .semibold))
            .tracking(1.1)
            .foregroundStyle(Palette.dim)
    }
}

struct StatCell: View {
    let label: String
    let value: String
    var tone: Color = Palette.ink
    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Eyebrow(text: label)
            Text(value)
                .font(.system(size: 16, weight: .light, design: .monospaced))
                .foregroundStyle(tone)
                .minimumScaleFactor(0.6)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct MiniStat: View {
    let label: String
    let value: String
    var tone: Color = Palette.ink
    var body: some View {
        HStack(spacing: 6) {
            Text(value)
                .font(.system(size: 12, design: .monospaced))
                .foregroundStyle(tone)
                .minimumScaleFactor(0.7)
                .lineLimit(1)
            Eyebrow(text: label)
        }
    }
}

struct CenteredNote: View {
    let title: String
    let sub: String
    var body: some View {
        VStack(spacing: 6) {
            Text(title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Palette.ink)
            Text(sub).font(.system(size: 11)).foregroundStyle(Palette.muted)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

func pnlColor(_ v: Double) -> Color { v >= 0 ? Palette.accent : Palette.down }

// MARK: - Portfolio widget (medium)

struct PortfolioView: View {
    let entry: PortfolioEntry
    var body: some View {
        switch entry.state {
        case .noWallet:
            CenteredNote(title: "PitchMarket", sub: "Open the app and connect a wallet")
        case .failure:
            CenteredNote(title: "Portfolio", sub: "Couldn’t reach the exchange")
        case .data(let s):
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .top, spacing: 14) {
                    // Available dominates the left half.
                    VStack(alignment: .leading, spacing: 4) {
                        Eyebrow(text: "Available")
                        Text(Fmt.usd(s.availableMicro))
                            .font(.system(size: 30, weight: .light, design: .monospaced))
                            .foregroundStyle(Palette.ink)
                            .minimumScaleFactor(0.5)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    // Compact stat stack on the right.
                    VStack(alignment: .leading, spacing: 6) {
                        MiniStat(label: "Value · BBP", value: Fmt.usd(s.valueMicro))
                        MiniStat(label: "Unrealised P&L",
                                 value: Fmt.signedUsd(s.unrealizedMicro),
                                 tone: pnlColor(s.unrealizedMicro))
                        MiniStat(label: "Realised P&L",
                                 value: Fmt.signedUsd(s.realizedMicro),
                                 tone: pnlColor(s.realizedMicro))
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                Spacer(minLength: 0)
                HStack(spacing: 4) {
                    Text("\(s.openOrders) open order\(s.openOrders == 1 ? "" : "s")")
                    Text("·").foregroundStyle(Palette.dim)
                    Text("\(Fmt.usd(s.lockedMicro)) locked")
                    Spacer()
                    Text(Fmt.asOf(entry.date)).foregroundStyle(Palette.dim)
                }
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(Palette.muted)
            }
        }
    }
}

struct PortfolioWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "PortfolioWidget", provider: PortfolioProvider()) { entry in
            PortfolioView(entry: entry)
                .containerBackground(Palette.bg, for: .widget)
                .widgetURL(URL(string: "pitchmarket://portfolio"))
        }
        .configurationDisplayName("Portfolio")
        .description("Balance, position value, and P&L.")
        .supportedFamilies([.systemMedium])
    }
}

// MARK: - Price chart (recent fills + dashed entry line)

struct PriceChart: View {
    let points: [PricePoint]
    let entry: Double // cents
    let gain: Bool
    var body: some View {
        GeometryReader { geo in
            let cents = points.map(\.cents) + [entry]
            let lo = max(0, (cents.min() ?? 0) - 3)
            let hi = min(100, (cents.max() ?? 100) + 3)
            let span = max(hi - lo, 1)
            let y: (Double) -> CGFloat = { c in
                geo.size.height * (1 - CGFloat((c - lo) / span))
            }
            ZStack {
                // dashed entry reference
                Path { p in
                    p.move(to: CGPoint(x: 0, y: y(entry)))
                    p.addLine(to: CGPoint(x: geo.size.width, y: y(entry)))
                }
                .stroke(Palette.dim, style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                // price line
                if points.count > 1 {
                    Path { p in
                        for (i, pt) in points.enumerated() {
                            let x = geo.size.width * CGFloat(i) / CGFloat(points.count - 1)
                            let pos = CGPoint(x: x, y: y(pt.cents))
                            if i == 0 { p.move(to: pos) } else { p.addLine(to: pos) }
                        }
                    }
                    .stroke(gain ? Palette.accent : Palette.down,
                            style: StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
                } else {
                    Path { p in
                        let level = y(points.first?.cents ?? entry)
                        p.move(to: CGPoint(x: 0, y: level))
                        p.addLine(to: CGPoint(x: geo.size.width, y: level))
                    }
                    .stroke(gain ? Palette.accent : Palette.down, lineWidth: 1.5)
                }
            }
        }
    }
}

// MARK: - Active-market widget (large)

struct PositionRow: View {
    let p: PositionCalc
    var body: some View {
        HStack(spacing: 8) {
            Text(p.title)
                .font(.system(size: 11))
                .foregroundStyle(Palette.muted)
                .lineLimit(1)
            Spacer(minLength: 4)
            Text("\(p.side) \(Fmt.shares(p.qty))")
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(p.side == "YES" ? Palette.accent : Palette.down)
            Text(Fmt.signedUsd(p.unrealizedMicro))
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(pnlColor(p.unrealizedMicro))
        }
    }
}

struct OrderRow: View {
    let o: OrderCalc
    var body: some View {
        HStack(spacing: 8) {
            Text(o.title)
                .font(.system(size: 11))
                .foregroundStyle(Palette.muted)
                .lineLimit(1)
            Spacer(minLength: 4)
            Text("\(o.side) \(o.outcome)")
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(o.outcome == "YES" ? Palette.accent : Palette.down)
            Text("\(Fmt.shares(o.remaining)) @ \(Fmt.cents(o.price))")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(Palette.ink)
        }
    }
}

struct ActiveMarketView: View {
    let entry: ActiveMarketEntry
    var body: some View {
        switch entry.state {
        case .noWallet:
            CenteredNote(title: "PitchMarket", sub: "Open the app and connect a wallet")
        case .failure:
            CenteredNote(title: "Active market", sub: "Couldn’t reach the exchange")
        case .data(let am) where am.top == nil:
            // Flat, but resting orders are still worth showing while they wait to fill.
            VStack(alignment: .leading, spacing: 8) {
                Text("No open positions")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Palette.ink)
                if am.summary.orders.isEmpty {
                    Text("Your biggest position shows up here")
                        .font(.system(size: 11))
                        .foregroundStyle(Palette.muted)
                    Spacer()
                } else {
                    Eyebrow(text: "Open orders — waiting to fill")
                    ForEach(Array(am.summary.orders.prefix(6).enumerated()), id: \.offset) { _, o in
                        OrderRow(o: o)
                    }
                    Spacer(minLength: 0)
                    Text(Fmt.asOf(entry.date))
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(Palette.dim)
                }
            }
        case .data(let am):
            let c = am.top!
            let gain = c.unrealizedMicro >= 0
            VStack(alignment: .leading, spacing: 8) {
                // header
                HStack(alignment: .top) {
                    Text(c.title)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Palette.ink)
                        .lineLimit(2)
                    Spacer()
                    Text("\(c.side) · \(Fmt.shares(c.qty))")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(c.side == "YES" ? Palette.accent : Palette.down)
                        .padding(.horizontal, 6).padding(.vertical, 3)
                        .background(Palette.line, in: RoundedRectangle(cornerRadius: 4))
                }
                // price block
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(Fmt.cents(c.cur))
                        .font(.system(size: 34, weight: .light, design: .monospaced))
                        .foregroundStyle(Palette.ink)
                    Text("bought \(Fmt.cents(c.entry))")
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(Palette.muted)
                }
                // pnl row
                HStack(spacing: 10) {
                    Text(Fmt.signedUsd(c.unrealizedMicro))
                        .font(.system(size: 15, design: .monospaced))
                        .foregroundStyle(pnlColor(c.unrealizedMicro))
                    Eyebrow(text: "Unrealised")
                    if c.realizedMicro != 0 {
                        Text(Fmt.signedUsd(c.realizedMicro))
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(pnlColor(c.realizedMicro))
                        Eyebrow(text: "Realised")
                    }
                    Spacer()
                }
                PriceChart(points: am.points, entry: c.entry, gain: gain)
                    .frame(maxHeight: .infinity)
                // Other open positions, biggest first.
                if am.summary.positions.count > 1 {
                    VStack(alignment: .leading, spacing: 4) {
                        Eyebrow(text: "Other positions")
                        ForEach(Array(am.summary.positions.dropFirst().prefix(2).enumerated()),
                                id: \.offset) { _, p in
                            PositionRow(p: p)
                        }
                    }
                }
                Text(Fmt.asOf(entry.date))
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(Palette.dim)
            }
            .widgetURL(URL(string: "pitchmarket://market/\(c.marketID)"))
        }
    }
}

struct ActiveMarketWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "ActiveMarketWidget", provider: ActiveMarketProvider()) { entry in
            ActiveMarketView(entry: entry)
                .containerBackground(Palette.bg, for: .widget)
        }
        .configurationDisplayName("Active market")
        .description("Your biggest position — price, entry, P&L, and chart.")
        .supportedFamilies([.systemLarge])
    }
}

// MARK: - Bundle

@main
struct PitchWidgetBundle: WidgetBundle {
    var body: some Widget {
        PortfolioWidget()
        ActiveMarketWidget()
    }
}
