import Foundation
import FacetManifestKit

/// Reduces the live `ManifestFrame` stream into per-section dictionaries a
/// SwiftUI view can render, the same last-write-wins-per-key reducer shape
/// `app.js`'s manifest handler uses on the PWA side — the point of this
/// spike is that this reducer, not any browser-specific API, is the only
/// renderer-neutral thing about the manifest.
@MainActor
public final class ManifestStore: ObservableObject {
    @Published public private(set) var me: JSONValue?
    @Published public private(set) var services: [JSONValue] = []
    @Published public private(set) var ops: [JSONValue] = []
    @Published public private(set) var tasks: [JSONValue] = []
    @Published public private(set) var instances: [JSONValue] = []
    @Published public private(set) var outbox: [OutboxEntry] = []
    @Published public var connected: Bool = false
    @Published public var statusMessage: String = "Connecting…"

    private var servicesByKey: [String: JSONValue] = [:]
    private var opsByKey: [String: JSONValue] = [:]
    private var tasksByKey: [String: JSONValue] = [:]
    private var instancesByKey: [String: JSONValue] = [:]
    private var outboxByRequestID: [String: OutboxEntry] = [:]
    private var feedClient: FeedClient?

    public init() {}

    /// Wires the write path: called once `FacetSwiftUISpikeApp.connect()`
    /// has a logged-in `FeedClient`, so `enqueue(operationType:payload:)`
    /// below has somewhere to send a write. The store owns the outbox
    /// lifecycle already (`apply`'s `.outbox` case), so it is the natural
    /// owner of the trigger too, not `ContentView` reaching into a client
    /// reference of its own.
    public func attach(feedClient: FeedClient) {
        self.feedClient = feedClient
    }

    /// Submits one write via the attached `FeedClient`. Fire-and-forget by
    /// design (facet-app-ux.md — "the browser does not block on the actual
    /// Gateway round-trip"): the outcome arrives back over the SSE stream as
    /// an `outbox` frame, not as this call's return value. A synchronous
    /// failure (no session, network) surfaces as a status message since
    /// there is no `requestId` yet to hang an outbox row off.
    public func enqueue(operationType: String, payload: JSONValue, authContext: JSONValue? = nil, optionalReads: [String] = []) async {
        guard let feedClient else { return }
        do {
            _ = try await feedClient.enqueue(
                operationType: operationType, payload: payload,
                optionalReads: optionalReads, authContext: authContext)
        } catch {
            statusMessage = "Enqueue failed: \(error)"
        }
    }

    public func apply(_ frame: ManifestFrame) {
        switch frame.kind {
        case "connectivity":
            connected = frame.connected
            return
        case "ready":
            statusMessage = "Live"
            return
        case "revoked":
            statusMessage = "Revoked: \(frame.reason)"
            return
        case "outbox":
            if let entry = frame.outbox {
                outboxByRequestID[entry.requestID] = entry
                outbox = outboxByRequestID.keys.sorted().compactMap { outboxByRequestID[$0] }
            }
            return
        case "manifest":
            break
        default:
            return // any future frame kind: out of this spike's scope
        }

        switch frame.section {
        case .identity:
            me = frame.deleted ? nil : frame.data
        case .service:
            apply(frame, to: &servicesByKey)
            services = sortedByKey(servicesByKey)
        case .opMeta:
            apply(frame, to: &opsByKey)
            ops = sortedByKey(opsByKey)
        case .task:
            apply(frame, to: &tasksByKey)
            tasks = sortedByKey(tasksByKey)
        case .instance:
            apply(frame, to: &instancesByKey)
            instances = sortedByKey(instancesByKey)
        case .other:
            break
        }
    }

    private func apply(_ frame: ManifestFrame, to dict: inout [String: JSONValue]) {
        if frame.deleted {
            dict.removeValue(forKey: frame.key)
        } else if let data = frame.data {
            dict[frame.key] = data
        }
    }

    private func sortedByKey(_ dict: [String: JSONValue]) -> [JSONValue] {
        dict.keys.sorted().compactMap { dict[$0] }
    }
}
