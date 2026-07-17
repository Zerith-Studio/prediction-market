import SwiftUI

// MARK: - Palette (mobile/tailwind.config.js — keep in lockstep)

enum Palette {
    static let bg = Color(hex: 0x0A0A0B)
    static let ink = Color(hex: 0xF4F5F7)
    static let muted = Color(hex: 0x9297A0)
    static let dim = Color(hex: 0x565B63)
    static let line = Color(hex: 0x1B1C20)
    static let accent = Color(hex: 0x34D399)
    static let down = Color(hex: 0xF2637E)
}

extension Color {
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }
}

// MARK: - Wire DTOs (backend/internal/api JSON, snake_case; numbers as Double
// defensively — Go may emit int or float)

struct WirePortfolio: Decodable {
    struct Balance: Decodable { let usdc_available: Double? }
    struct Position: Decodable {
        let market_id: String
        let yes: Double?
        let no: Double?
        let avg_cost: Double?
        let realized: Double?
        let best_bid: Double?
    }
    struct Order: Decodable {
        let market_id: String?
        let outcome: Double?   // 0 NO, 1 YES
        let side: Double?      // 0 buy, 1 sell
        let price: Double?
        let remaining: Double?
        let status: String?
    }
    let balance: Balance?
    let positions: [Position]?
    let orders: [Order]?
}

struct WireMarkets: Decodable {
    struct Market: Decodable {
        let market_id: String
        let title: String?
    }
    let markets: [Market]?
}

struct WireFills: Decodable {
    struct Fill: Decodable {
        let price: Double?
        let ts: String?
    }
    let fills: [Fill]?
}

// MARK: - View models + PnL math (mirrors frontend/app/portfolio/page.tsx calc())

struct PositionCalc {
    let marketID: String
    var title: String
    let side: String      // "YES" | "NO"
    let qty: Double       // shares
    let entry: Double     // avg cost, cents (in held side's terms)
    let cur: Double       // exit mark (BBP), cents
    let valueMicro: Double
    let unrealizedMicro: Double
    let realizedMicro: Double
}

struct OrderCalc {
    let title: String
    let side: String    // "BUY" | "SELL"
    let outcome: String // "YES" | "NO"
    let price: Double   // cents
    let remaining: Double
}

struct PortfolioSummary {
    let availableMicro: Double
    let valueMicro: Double
    let unrealizedMicro: Double
    let realizedMicro: Double
    let lockedMicro: Double
    let openOrders: Int
    let positions: [PositionCalc] // open only, sorted by valueMicro desc
    let orders: [OrderCalc]       // live resting orders
}

struct PricePoint {
    let date: Date
    let cents: Double
}

struct ActiveMarketData {
    let summary: PortfolioSummary
    let top: PositionCalc?    // biggest open position; nil when flat
    let points: [PricePoint]  // recent fills for top's market
}

enum LoadState<T> {
    case noWallet
    case failure
    case data(T)
}

func summarize(_ w: WirePortfolio, titles: [String: String]) -> PortfolioSummary {
    var positions: [PositionCalc] = []
    var realizedTotal = 0.0
    for p in w.positions ?? [] {
        let yes = p.yes ?? 0, no = p.no ?? 0
        realizedTotal += p.realized ?? 0
        guard yes > 0 || no > 0 else { continue }
        let side = yes > 0 ? "YES" : "NO"
        let qty = yes > 0 ? yes : no
        let avg = p.avg_cost ?? 0
        let bb = p.best_bid ?? 0
        let cur = bb > 0 ? (side == "YES" ? bb : 100 - bb) : avg
        let value = qty * cur * 10_000
        positions.append(PositionCalc(
            marketID: p.market_id,
            title: titles[p.market_id] ?? shortID(p.market_id),
            side: side,
            qty: qty,
            entry: avg,
            cur: cur,
            valueMicro: value,
            unrealizedMicro: value - qty * avg * 10_000,
            realizedMicro: p.realized ?? 0
        ))
    }
    positions.sort { $0.valueMicro > $1.valueMicro }
    let live = (w.orders ?? []).filter { $0.status == "live" }
    let locked = live
        .filter { ($0.side ?? 0) == 0 } // BUY soft-locks USDC
        .reduce(0.0) { $0 + ($1.remaining ?? 0) * ($1.price ?? 0) * 10_000 }
    let orders = live.map { o in
        OrderCalc(
            title: o.market_id.flatMap { titles[$0] ?? shortID($0) } ?? "—",
            side: (o.side ?? 0) == 0 ? "BUY" : "SELL",
            outcome: (o.outcome ?? 1) == 1 ? "YES" : "NO",
            price: o.price ?? 0,
            remaining: o.remaining ?? 0
        )
    }
    return PortfolioSummary(
        availableMicro: w.balance?.usdc_available ?? 0,
        valueMicro: positions.reduce(0) { $0 + $1.valueMicro },
        unrealizedMicro: positions.reduce(0) { $0 + $1.unrealizedMicro },
        realizedMicro: realizedTotal,
        lockedMicro: locked,
        openOrders: live.count,
        positions: positions,
        orders: orders
    )
}

func shortID(_ hex: String) -> String {
    hex.count > 10 ? "\(hex.prefix(6))…\(hex.suffix(4))" : hex
}

// MARK: - Loader

struct WidgetLoader {
    static let appGroup = "group.com.pitchmarket.app"

    static func config() -> (wallet: String, apiURL: String)? {
        let d = UserDefaults(suiteName: appGroup)
        guard let w = d?.string(forKey: "wallet"), !w.isEmpty,
              let u = d?.string(forKey: "apiUrl"), !u.isEmpty
        else { return nil }
        return (w, u)
    }

    static func fetch<T: Decodable>(_ url: URL, as type: T.Type) async throws -> T {
        var req = URLRequest(url: url)
        req.timeoutInterval = 10
        let (data, resp) = try await URLSession.shared.data(for: req)
        guard let http = resp as? HTTPURLResponse, http.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    static func loadPortfolio() async -> LoadState<PortfolioSummary> {
        guard let cfg = config() else { return .noWallet }
        guard let pfURL = URL(string: "\(cfg.apiURL)/portfolio?wallet=\(cfg.wallet)"),
              let mkURL = URL(string: "\(cfg.apiURL)/markets")
        else { return .failure }
        do {
            let pf = try await fetch(pfURL, as: WirePortfolio.self)
            // Titles are a nice-to-have — portfolio still renders with ids.
            let titles = (try? await fetch(mkURL, as: WireMarkets.self))
                .flatMap { m -> [String: String]? in
                    var t: [String: String] = [:]
                    for mk in m.markets ?? [] { t[mk.market_id] = mk.title }
                    return t
                } ?? [:]
            return .data(summarize(pf, titles: titles))
        } catch {
            return .failure
        }
    }

    static func loadActiveMarket() async -> LoadState<ActiveMarketData> {
        switch await loadPortfolio() {
        case .noWallet: return .noWallet
        case .failure: return .failure
        case .data(let s):
            guard let top = s.positions.first else {
                return .data(ActiveMarketData(summary: s, top: nil, points: []))
            }
            guard let cfg = config(),
                  let url = URL(string: "\(cfg.apiURL)/markets/\(top.marketID)/fills")
            else { return .data(ActiveMarketData(summary: s, top: top, points: [])) }
            let wire = (try? await fetch(url, as: WireFills.self))?.fills ?? []
            let iso = ISO8601DateFormatter()
            iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            let isoPlain = ISO8601DateFormatter()
            var points: [PricePoint] = wire.compactMap { f in
                guard let p = f.price, let ts = f.ts,
                      let d = iso.date(from: ts) ?? isoPlain.date(from: ts)
                else { return nil }
                return PricePoint(date: d, cents: p) // fills are in YES terms
            }
            points.sort { $0.date < $1.date }
            return .data(ActiveMarketData(summary: s, top: top, points: points))
        }
    }
}

// MARK: - Formatting

enum Fmt {
    static func usd(_ micro: Double) -> String {
        let f = NumberFormatter()
        f.numberStyle = .currency
        f.currencyCode = "USD"
        f.maximumFractionDigits = 2
        return f.string(from: NSNumber(value: micro / 1_000_000)) ?? "$0.00"
    }
    static func signedUsd(_ micro: Double) -> String {
        (micro >= 0 ? "+" : "−") + usd(abs(micro))
    }
    static func cents(_ c: Double) -> String {
        "\(Int(c.rounded()))¢"
    }
    static func shares(_ q: Double) -> String {
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.maximumFractionDigits = 0
        return f.string(from: NSNumber(value: q)) ?? "0"
    }
    static func asOf(_ d: Date) -> String {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return "as of \(f.string(from: d))"
    }
}
