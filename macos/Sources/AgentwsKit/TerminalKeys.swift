public enum TerminalKeys {
    public static func goesToPane(key: String, command: Bool, control: Bool) -> Bool {
        if command { return false }
        if control && key == " " { return false }
        return true
    }
}
