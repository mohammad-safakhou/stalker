import Combine
import Darwin
@preconcurrency import Foundation

@MainActor
public final class BonjourDiscovery: NSObject, ObservableObject {
    @Published public private(set) var services: [DiscoveredStalker] = []

    private let browser = NetServiceBrowser()
    private var resolving: Set<NetService> = []

    public override init() {
        super.init()
        browser.delegate = self
    }

    public func start() {
        browser.searchForServices(ofType: "_stalker._tcp.", inDomain: "local.")
    }

    public func stop() {
        browser.stop()
        resolving.removeAll()
        services.removeAll()
    }
}

public struct DiscoveredStalker: Identifiable, Equatable, Sendable {
    public var id: String
    public var name: String
    public var baseURL: URL
    public var deviceID: String?
}

extension BonjourDiscovery: @preconcurrency NetServiceBrowserDelegate, @preconcurrency NetServiceDelegate {
    public func netServiceBrowser(_ browser: NetServiceBrowser, didFind service: NetService, moreComing: Bool) {
        service.delegate = self
        resolving.insert(service)
        service.resolve(withTimeout: 5)
    }

    public func netServiceDidResolveAddress(_ sender: NetService) {
        resolving.remove(sender)
        guard sender.port > 0 else { return }
        let host = preferredHost(for: sender) ?? sender.hostName
        guard let host else { return }
        guard let url = URL(string: "http://\(urlHost(host)):\(sender.port)") else { return }
        let txt = sender.txtRecordData().map(NetService.dictionary(fromTXTRecord:)) ?? [:]
        let deviceID = txt["device_id"].flatMap { String(data: $0, encoding: .utf8) }
        let discovered = DiscoveredStalker(id: sender.name + "@" + host, name: sender.name, baseURL: url, deviceID: deviceID)
        if !services.contains(discovered) {
            services.append(discovered)
        }
    }

    private func preferredHost(for service: NetService) -> String? {
        let hosts = (service.addresses ?? []).compactMap(numericHost)
        return hosts.first { $0.contains(".") } ?? hosts.first
    }

    private func numericHost(from data: Data) -> String? {
        data.withUnsafeBytes { rawBuffer -> String? in
            guard let base = rawBuffer.baseAddress else { return nil }
            let sockaddrPointer = base.assumingMemoryBound(to: sockaddr.self)
            var hostBuffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            let result = getnameinfo(
                sockaddrPointer,
                socklen_t(data.count),
                &hostBuffer,
                socklen_t(hostBuffer.count),
                nil,
                0,
                NI_NUMERICHOST
            )
            guard result == 0 else { return nil }
            let end = hostBuffer.firstIndex(of: 0) ?? hostBuffer.count
            return String(decoding: hostBuffer[..<end].map(UInt8.init(bitPattern:)), as: UTF8.self)
        }
    }

    private func urlHost(_ host: String) -> String {
        host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
    }
}
