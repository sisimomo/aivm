# ============================================================
# aivm — Demo Trailer Tape
# https://github.com/charmbracelet/vhs
#
# Record (does everything automatically):
#   make demo
# ============================================================

Output demo/aivm.mp4

Set FontSize 14
Set Width 1200
Set Height 600
Set Theme "Dracula"
Set FontFamily "JetBrains Mono"
Set TypingSpeed 80ms
Set Padding 20

# ── Hidden setup: clean prompt ────────────────────────────────────────
Hide
Set TypingSpeed 1ms
Type `export PS1="~/dev/my-project $ "`
Enter
Ctrl+L
Set TypingSpeed 80ms
Show

# ============================================================
# ACT 1: Bootstrap — first boot, tools installed automatically
# ============================================================

Sleep 500ms
Type "aivm"
Enter
Sleep 115s
Ctrl+C
Sleep 2s

Type "aivm status"
Enter
Sleep 3s

Type "cat ~/.aivm/aivm.yaml"
Enter
Sleep 4s

# ============================================================
# ACT 2: Parallel sessions — one VM, many projects
# ============================================================

Sleep 1s
Ctrl+L
Sleep 500ms
Type "# One VM — run agents across as many projects as you need"
Enter
Sleep 500ms
Type "# Multiple agents — install once, switch anytime"
Enter
Sleep 1.5s

Set TypingSpeed 1ms
Type `tmux new-session -d -s aivm-demo \; split-window -h \; set -g status off \; set -g pane-border-style 'fg=black' \; set -g pane-active-border-style 'fg=black'`
Enter
Sleep 100ms
Type "tmux attach -t aivm-demo"
Enter

Set TypingSpeed 80ms
Sleep 1s

Type "aivm"
Enter

Ctrl+B
Sleep 100ms
Right
Sleep 500ms

Type "aivm --agent copilot"
Enter
Sleep 5s

Ctrl+B
Sleep 300ms
Type "d"
Sleep 1s

Type "aivm status"
Enter
Sleep 3s

Hide
Set TypingSpeed 1ms
Type "tmux kill-session -t aivm-demo 2>/dev/null; true"
Enter
Sleep 300ms
Set TypingSpeed 80ms
Show
Ctrl+L

# ============================================================
# ACT 3: Security — isolated VM, yolo-friendly
# ============================================================

Sleep 1s
Type "# All agents run inside an isolated VM — not on your host"
Enter
Sleep 1.5s
Type "# Only the folders you mount are reachable — nothing else"
Enter
Sleep 1.5s
Type "# VM is fully disposable — recreate in minutes if anything goes wrong or you want a clean start"
Enter
Sleep 3s

# ============================================================
# ACT 4: Plugins — any tool, any skill, custom definitions
# ============================================================

Sleep 1s
Ctrl+L
Sleep 500ms
Type "# aivm plugins bootstrap your VM: mise-node, mise-go, mise-python..."
Enter
Sleep 1.5s
Type "# Add 'skills' to install agent skills from any GitHub repo"
Enter
Sleep 1.5s
Type "# Define custom aivm plugins or override built-ins for any need"
Enter
Sleep 3s

Type "# Prefer a web UI? Enable T3 Code — one line in aivm.yaml"
Enter
Sleep 2.5s

Type "aivm stop"
Enter
Sleep 4s
