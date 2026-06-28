//
//  ContentView.swift
//  Stalker
//
//  Created by Safa Safakhou on 25.06.26.
//

import SwiftUI
import StalkerPhone
import StalkerShared

struct ContentView: View {
    var body: some View {
#if os(iOS)
        StalkerPhoneRootView()
#else
        MacDashboardView()
#endif
    }
}

private struct MacDashboardView: View {
    @StateObject private var viewModel = StatsViewModel()

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if let error = viewModel.errorMessage {
                        Label(error, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                    }

                    StatsDashboardView(
                        snapshot: viewModel.snapshot,
                        isLive: viewModel.isLive
                    )
                }
                .frame(maxWidth: 900, alignment: .leading)
                .padding()
            }
            .navigationTitle("Stalker")
            .toolbar {
                Button {
                    Task {
                        await viewModel.refresh()
                    }
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
            }
        }
        .task {
            await viewModel.refresh()
            viewModel.connectLive()
        }
    }
}
