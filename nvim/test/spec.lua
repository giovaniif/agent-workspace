vim.opt.rtp:prepend(assert(vim.env.CREW_NVIM_PLUGIN, "CREW_NVIM_PLUGIN is not set"))
vim.cmd("runtime plugin/crew.lua")

local failed = 0

local function test(name, fn)
  local ok, err = pcall(fn)
  if ok then
    print("ok   " .. name)
  else
    failed = failed + 1
    print("FAIL " .. name .. "\n     " .. tostring(err))
  end
end

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s\n     got:  %s\n     want: %s"):format(what or "not equal", vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function write(path, text)
  vim.fn.mkdir(vim.fs.dirname(path), "p")
  local f = assert(io.open(path, "w"))
  f:write(text)
  f:close()
end

local function real(path)
  return assert(vim.uv.fs_realpath(path))
end

local root = vim.fn.tempname()
vim.fn.mkdir(root, "p")
root = real(root)

local seq = 0
local function fresh()
  seq = seq + 1
  local dir = root .. "/case" .. seq
  vim.fn.mkdir(dir, "p")
  local bin = dir .. "/crew"
  write(
    bin,
    [[#!/bin/sh
printf '%s\0' "$@" >> "$FAKE_LOG"
if [ -n "$FAKE_STDOUT" ]; then printf '%s' "$FAKE_STDOUT"; fi
if [ -n "$FAKE_EXIT" ]; then echo "boom: $FAKE_EXIT" >&2; exit 1; fi
]]
  )
  vim.uv.fs_chmod(bin, 493)
  vim.env.FAKE_LOG = dir .. "/log"
  vim.env.FAKE_STDOUT = nil
  vim.env.FAKE_EXIT = nil
  vim.env.CREW_SESSION = "s1"
  local notes = {}
  vim.notify = function(msg, level)
    table.insert(notes, { msg = msg, level = level or vim.log.levels.INFO })
  end
  vim.ui.input = function(_, cb)
    cb(vim.env.FAKE_INPUT)
  end
  vim.env.FAKE_INPUT = "why not const?"
  pcall(vim.api.nvim_del_user_command, "DiffviewOpen")
  vim.cmd("silent! %bwipeout!")
  require("crew").setup({ bin = bin, navigate = false })
  return dir, notes
end

local function calls(dir)
  local f = io.open(dir .. "/log", "rb")
  if not f then
    return {}
  end
  local data = f:read("*a")
  f:close()
  local out = {}
  for part in data:gmatch("([^%z]*)%z") do
    table.insert(out, part)
  end
  return out
end

local function has_note(notes, level, needle)
  for _, n in ipairs(notes) do
    if n.level == level and n.msg:find(needle, 1, true) then
      return true
    end
  end
  return false
end

test("the commands exist before setup is called", function()
  eq(vim.fn.exists(":CrewDiff"), 2, ":CrewDiff")
  eq(vim.fn.exists(":CrewComment"), 2, ":CrewComment")
end)

test("a commented range goes to the CLI with its file, lines, code and comment", function()
  local dir = fresh()
  local file = dir .. "/wt/src/a.lua"
  write(file, "one\ntwo\nthree\nfour\n")
  vim.cmd.edit(file)
  vim.cmd("2,3CrewComment")
  eq(calls(dir), {
    "review", "comment", "--session", "s1", "--file", real(file),
    "--start", "2", "--end", "3", "--code", "two\nthree", "--body", "why not const?",
  }, "crew argv")
end)

test("a comment given as command arguments is not asked for", function()
  local dir = fresh()
  vim.env.FAKE_INPUT = nil
  local file = dir .. "/wt/b.lua"
  write(file, "a\nb\n")
  vim.cmd.edit(file)
  vim.cmd("2CrewComment rename this")
  local argv = calls(dir)
  eq(argv[#argv], "rename this", "body")
  eq({ argv[9], argv[10] }, { "--end", "2" }, "a single line ends where it starts")
end)

test("a cancelled or empty comment sends nothing", function()
  local dir = fresh()
  local file = dir .. "/wt/c.lua"
  write(file, "a\n")
  vim.cmd.edit(file)
  vim.env.FAKE_INPUT = nil
  vim.cmd("1CrewComment")
  vim.env.FAKE_INPUT = "   "
  vim.cmd("1CrewComment")
  eq(calls(dir), {}, "crew calls")
end)

test("a comment on a buffer with no file is refused", function()
  local dir, notes = fresh()
  vim.cmd.enew()
  vim.cmd("1CrewComment hi")
  eq(calls(dir), {}, "crew calls")
  eq(has_note(notes, vim.log.levels.ERROR, "no file"), true, "an error naming the problem")
end)

test("a comment without a session says where the session comes from", function()
  local dir, notes = fresh()
  vim.env.CREW_SESSION = nil
  local file = dir .. "/wt/d.lua"
  write(file, "a\n")
  vim.cmd.edit(file)
  vim.cmd("1CrewComment hi")
  eq(calls(dir), {}, "crew calls")
  eq(has_note(notes, vim.log.levels.ERROR, "CREW_SESSION"), true, "an error naming CREW_SESSION")
end)

test("a CLI failure is reported with its message", function()
  local dir, notes = fresh()
  vim.env.FAKE_EXIT = "1"
  local file = dir .. "/wt/e.lua"
  write(file, "a\n")
  vim.cmd.edit(file)
  vim.cmd("1CrewComment hi")
  eq(has_note(notes, vim.log.levels.ERROR, "boom"), true, "the CLI's stderr in an error")
end)

test("a comment that lands is confirmed", function()
  local dir, notes = fresh()
  vim.env.FAKE_STDOUT = "comment abc on a.lua:1\n"
  local file = dir .. "/wt/f.lua"
  write(file, "a\n")
  vim.cmd.edit(file)
  vim.cmd("1CrewComment hi")
  eq(has_note(notes, vim.log.levels.INFO, "comment abc on a.lua:1"), true, "the CLI's answer as an info")
end)

local function scope_json(dir)
  return vim.json.encode({
    { worktree = "w-api", path = real(dir) .. "/api", from = "abc123", files = { "a.go" } },
    { worktree = "w-web", path = real(dir) .. "/web", from = "def456", files = { "b.ts" } },
    { worktree = "w-old", path = real(dir) .. "/old", from = "", files = {}, error = "no turn yet" },
  })
end

local function fake_diffview()
  local seen = {}
  vim.api.nvim_create_user_command("DiffviewOpen", function(o)
    table.insert(seen, o.fargs)
  end, { nargs = "*" })
  return seen
end

test("CrewDiff opens diffview on the worktree the buffer is in, from the scope's base", function()
  local dir = fresh()
  vim.fn.mkdir(dir .. "/api", "p")
  vim.fn.mkdir(dir .. "/web", "p")
  vim.fn.mkdir(dir .. "/old", "p")
  vim.env.FAKE_STDOUT = scope_json(dir)
  local seen = fake_diffview()
  local file = dir .. "/web/x.lua"
  write(file, "a\n")
  vim.cmd.edit(file)
  vim.cmd("CrewDiff")
  eq(calls(dir), { "review", "scope", "--session", "s1" }, "crew argv without a scope")
  eq(seen, { { "def456", "--untracked-files=true", "-C" .. real(dir) .. "/web" } }, "DiffviewOpen args")
end)

test("CrewDiff passes the scope it is given", function()
  local dir = fresh()
  vim.fn.mkdir(dir .. "/api", "p")
  vim.env.FAKE_STDOUT = scope_json(dir)
  fake_diffview()
  vim.cmd.cd(dir .. "/api")
  vim.cmd("CrewDiff last_turn")
  eq(calls(dir), { "review", "scope", "--session", "s1", "--scope", "last_turn" }, "crew argv")
end)

test("CrewDiff falls back to the first worktree that has a base when the buffer is in none", function()
  local dir = fresh()
  vim.env.FAKE_STDOUT = scope_json(dir)
  local seen = fake_diffview()
  vim.cmd.cd("/")
  vim.cmd.enew()
  vim.cmd("CrewDiff")
  eq(seen, { { "abc123", "--untracked-files=true", "-C" .. real(dir) .. "/api" } }, "DiffviewOpen args")
end)

test("CrewDiff reports a scope that has no base anywhere", function()
  local dir, notes = fresh()
  vim.env.FAKE_STDOUT = vim.json.encode({ { worktree = "w", path = dir, from = "", files = {}, error = "no turn yet" } })
  local seen = fake_diffview()
  vim.cmd("CrewDiff last_turn")
  eq(seen, {}, "DiffviewOpen calls")
  eq(has_note(notes, vim.log.levels.ERROR, "no turn yet"), true, "the daemon's reason")
end)

test("CrewDiff says so when diffview is not installed", function()
  local dir, notes = fresh()
  vim.env.FAKE_STDOUT = scope_json(dir)
  vim.cmd("CrewDiff")
  eq(has_note(notes, vim.log.levels.ERROR, "diffview.nvim"), true, "an error naming diffview.nvim")
end)

test("CrewDiff without a session says where the session comes from", function()
  local dir, notes = fresh()
  vim.env.CREW_SESSION = nil
  fake_diffview()
  vim.cmd("CrewDiff")
  eq(calls(dir), {}, "crew calls")
  eq(has_note(notes, vim.log.levels.ERROR, "CREW_SESSION"), true, "an error naming CREW_SESSION")
end)

local function layout_2x2()
  vim.cmd("silent! only")
  vim.cmd("vsplit")
  vim.cmd("split")
  vim.cmd("wincmd l")
  vim.cmd("split")
  vim.cmd("wincmd t")
end

local function fake_tmux(dir)
  local tmux = dir .. "/tmux"
  write(tmux, '#!/bin/sh\nprintf \'%s\\0\' "$@" >> "$FAKE_TMUX_LOG"\n')
  vim.uv.fs_chmod(tmux, 493)
  vim.env.FAKE_TMUX_LOG = dir .. "/tmux.log"
  return tmux
end

local function tmux_calls(dir)
  local f = io.open(dir .. "/tmux.log", "rb")
  if not f then
    return {}
  end
  local data = f:read("*a")
  f:close()
  local out = {}
  for part in data:gmatch("([^%z]*)%z") do
    table.insert(out, part)
  end
  return out
end

test("navigation moves between nvim splits and asks tmux only at the edge", function()
  local dir = fresh()
  local tmux = fake_tmux(dir)
  vim.env.TMUX = "/tmp/fake,1,0"
  require("crew").setup({ tmux = tmux, navigate = false })
  local crew = require("crew")
  layout_2x2()
  local top_left = vim.api.nvim_get_current_win()

  crew.navigate("l")
  local top_right = vim.api.nvim_get_current_win()
  assert(top_right ~= top_left, "l did not leave the left split")
  crew.navigate("j")
  local bottom_right = vim.api.nvim_get_current_win()
  assert(bottom_right ~= top_right, "j did not leave the top split")
  crew.navigate("h")
  assert(vim.api.nvim_get_current_win() ~= bottom_right, "h did not leave the right split")
  eq(tmux_calls(dir), {}, "tmux was called while nvim still had splits to move to")

  vim.api.nvim_set_current_win(top_left)
  crew.navigate("h")
  crew.navigate("k")
  eq(vim.api.nvim_get_current_win(), top_left, "moving past the edge must stay in place")
  eq(tmux_calls(dir), {
    "if", "-F", "#{pane_at_left}", "", "select-pane -L",
    "if", "-F", "#{pane_at_top}", "", "select-pane -U",
  }, "tmux at the top-left corner")

  vim.api.nvim_set_current_win(bottom_right)
  crew.navigate("l")
  crew.navigate("j")
  local argv = tmux_calls(dir)
  eq({ unpack(argv, 11) }, {
    "if", "-F", "#{pane_at_right}", "", "select-pane -R",
    "if", "-F", "#{pane_at_bottom}", "", "select-pane -D",
  }, "tmux at the bottom-right corner")
end)

test("navigation outside tmux stays inside nvim", function()
  local dir = fresh()
  local tmux = fake_tmux(dir)
  vim.env.TMUX = nil
  require("crew").setup({ tmux = tmux, navigate = false })
  vim.cmd("silent! only")
  require("crew").navigate("h")
  eq(tmux_calls(dir), {}, "tmux calls")
end)

test("setup maps C-h/j/k/l unless navigation is turned off", function()
  fresh()
  for _, k in ipairs({ "h", "j", "k", "l" }) do
    pcall(vim.keymap.del, "n", "<C-" .. k .. ">")
  end
  require("crew").setup({ navigate = false })
  eq(vim.fn.maparg("<C-h>", "n"), "", "a mapping with navigate = false")
  require("crew").setup({})
  for _, k in ipairs({ "h", "j", "k", "l" }) do
    assert(vim.fn.maparg("<C-" .. k .. ">", "n", false, true).callback ~= nil, "<C-" .. k .. "> is not mapped")
  end
end)

vim.fn.delete(root, "rf")
if failed > 0 then
  print(("%d spec(s) failed"):format(failed))
  vim.cmd("cquit 1")
end
print("all specs passed")
