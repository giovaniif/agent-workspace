#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

struct PermissionCardView: View {
    let card: PermissionCard
    let answer: (String) -> Void
    let hide: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Text("◆").foregroundStyle(theme(.red))
                Text("Needs permission").font(.system(size: 12, weight: .semibold))
                Spacer()
                Button("Show terminal", action: hide)
                    .buttonStyle(.plain)
                    .font(.system(size: 11))
                    .foregroundStyle(theme(.subtext))
                    .keyboardShortcut(.escape, modifiers: [])
            }
            Text(card.prompt.text)
                .font(Metrics.code)
                .lineLimit(4)
                .textSelection(.enabled)
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(card.prompt.choices.enumerated()), id: \.element.id) { index, choice in
                    choiceButton(index, choice, theme)
                }
            }
        }
        .padding(14)
        .frame(maxWidth: 560, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).fill(theme(.mantle)))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(theme(.red), lineWidth: 1.5))
        .shadow(color: .black.opacity(0.18), radius: 10, y: 3)
    }

    @ViewBuilder
    private func choiceButton(_ index: Int, _ choice: PromptChoice, _ theme: Theme) -> some View {
        let first = index == 0
        let button = Button {
            answer(choice.id)
        } label: {
            HStack(spacing: 8) {
                Text("\(index + 1)")
                    .font(Metrics.mono)
                    .frame(width: 18, height: 18)
                    .background(RoundedRectangle(cornerRadius: 4).fill(first ? theme(.base).opacity(0.25) : theme(.surface)))
                Text(choice.label).font(.system(size: 12, weight: first ? .semibold : .regular)).lineLimit(1)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(RoundedRectangle(cornerRadius: 6).fill(first ? theme(.red) : theme(.crust)))
            .foregroundStyle(first ? theme(.base) : theme(.text))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        if index < 9 {
            button.keyboardShortcut(KeyEquivalent(Character("\(index + 1)")), modifiers: [])
        } else {
            button
        }
    }
}

struct AttentionMessage: View {
    let text: String
    let dismiss: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.circle").foregroundStyle(theme(.peach))
            Text(text).font(.system(size: 12))
            Button(action: dismiss) {
                Image(systemName: "xmark").font(.system(size: 10, weight: .semibold))
            }
            .buttonStyle(.plain)
            .foregroundStyle(theme(.subtext))
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .background(Capsule().fill(theme(.mantle)))
        .overlay(Capsule().stroke(theme(.peach)))
    }
}
#endif
