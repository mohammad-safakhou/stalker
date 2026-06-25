import StalkerShared
import SwiftUI

public struct StalkerWatchRootView: View {
    @StateObject private var viewModel = StatsViewModel()

    public init() {}

    public var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Label(viewModel.isLive ? "Live" : "Synced", systemImage: viewModel.isLive ? "bolt.fill" : "icloud")
                    .font(.caption)
                    .foregroundStyle(viewModel.isLive ? .green : .secondary)
                Text(TokenFormat.compact(viewModel.snapshot?.totals.totalTokens ?? 0))
                    .font(.system(.largeTitle, design: .rounded).weight(.bold))
                Text("tokens")
                    .foregroundStyle(.secondary)
                HStack {
                    VStack(alignment: .leading) {
                        Text("In").font(.caption2)
                        Text(TokenFormat.compact(viewModel.snapshot?.totals.inputTokens ?? 0))
                    }
                    Spacer()
                    VStack(alignment: .trailing) {
                        Text("Out").font(.caption2)
                        Text(TokenFormat.compact(viewModel.snapshot?.totals.outputTokens ?? 0))
                    }
                }
            }
            .padding()
        }
        .task {
            await viewModel.refresh()
            viewModel.connectLive()
        }
    }
}
