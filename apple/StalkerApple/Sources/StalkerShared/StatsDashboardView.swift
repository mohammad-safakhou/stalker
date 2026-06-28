import SwiftUI

public struct StatsDashboardView: View {
    public let snapshot: SyncSnapshot?
    public let isLive: Bool

    public init(snapshot: SyncSnapshot?, isLive: Bool) {
        self.snapshot = snapshot
        self.isLive = isLive
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Label(isLive ? "Live" : "Synced", systemImage: isLive ? "bolt.fill" : "icloud")
                    .foregroundStyle(isLive ? .green : .secondary)
                Spacer()
                Text(snapshot?.device.name ?? "No device")
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: 12) {
                TokenMetric(title: "Input", value: snapshot?.totals.inputTokens ?? 0, color: .teal)
                TokenMetric(title: "Output", value: snapshot?.totals.outputTokens ?? 0, color: .purple)
            }
            HStack(spacing: 12) {
                TokenMetric(title: "Chars", value: snapshot?.totals.totalChars ?? 0, color: .orange)
                TokenMetric(title: "Rate/s", value: Int64(snapshot?.live.tokensPerSecond.rounded() ?? 0), color: .green)
            }

            BucketBars(title: "Today", buckets: snapshot?.hourly ?? [])
            TokenList(title: "Top input", tokens: snapshot?.totals.top.input ?? [])
            TokenList(title: "Top output", tokens: snapshot?.totals.top.output ?? [])
            TextList(title: "Top words", values: snapshot?.totals.topWords.input ?? [])
            TextList(title: "Top chars", values: snapshot?.totals.topChars.input ?? [])
        }
        .padding()
    }
}

private struct TokenMetric: View {
    let title: String
    let value: Int64
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.caption).foregroundStyle(.secondary)
            Text(TokenFormat.compact(value))
                .font(.system(.largeTitle, design: .rounded).weight(.semibold))
                .foregroundStyle(color)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 8))
    }
}

private struct BucketBars: View {
    let title: String
    let buckets: [StatsBucket]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.headline)
            HStack(alignment: .bottom, spacing: 4) {
                ForEach(buckets.suffix(24)) { bucket in
                    Capsule()
                        .fill(.teal.gradient)
                        .frame(width: 6, height: max(4, CGFloat(bucket.totalTokens) / CGFloat(maxToken) * 76))
                }
            }
            .frame(height: 82, alignment: .bottom)
        }
    }

    private var maxToken: Int64 {
        max(1, buckets.map(\.totalTokens).max() ?? 1)
    }
}

private struct TokenList: View {
    let title: String
    let tokens: [TokenBurn]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.headline)
            ForEach(tokens.prefix(5)) { token in
                HStack {
                    Text(token.token).lineLimit(1)
                    Spacer()
                    Text(TokenFormat.compact(token.occurrences)).foregroundStyle(.secondary)
                }
                .font(.callout.monospaced())
            }
        }
    }
}

private struct TextList: View {
    let title: String
    let values: [TextBurn]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.headline)
            ForEach(values.prefix(5)) { value in
                HStack {
                    Text(value.value).lineLimit(1)
                    Spacer()
                    Text(TokenFormat.compact(value.occurrences)).foregroundStyle(.secondary)
                }
                .font(.callout.monospaced())
            }
        }
    }
}
