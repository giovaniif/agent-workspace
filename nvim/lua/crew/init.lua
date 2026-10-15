local M = {}

local defaults = {
  bin = "crew",
  tmux = "tmux",
  navigate = true,
}

M.config = vim.deepcopy(defaults)

local navigation = {
  h = { key = "<C-h>", edge = "left", pane = "L" },
  j = { key = "<C-j>", edge = "bottom", pane = "D" },
  k = { key = "<C-k>", edge = "top", pane = "U" },
  l = { key = "<C-l>", edge = "right", pane = "R" },
}

local mapped = false

local function say(msg, level)
  vim.notify("crew: " .. msg, level or vim.log.levels.INFO)
end

local function fail(msg)
  say(msg, vim.log.levels.ERROR)
end

local function run(args)
  local cmd = { M.config.bin }
  vim.list_extend(cmd, args)
  local ok, res = pcall(function()
    return vim.system(cmd, { text = true }):wait(10000)
  end)
  if not ok then
    return nil, tostring(res)
  end
  if res.code ~= 0 then
    local msg = vim.trim(res.stderr or "")
    return nil, msg ~= "" and msg or vim.trim(res.stdout or "")
  end
  return res.stdout
end

local function session()
  local id = vim.env.CREW_SESSION
  if id == nil or id == "" then
    id = require("crew.legacy").session()
  end
  if id == nil or id == "" then
    fail("CREW_SESSION is not set; run nvim in the session's nvim pane (e in the crew TUI)")
    return nil
  end
  return id
end

local function real(path)
  return vim.uv.fs_realpath(path) or path
end

local function inside(dir, path)
  return path == dir or vim.startswith(path, dir .. "/")
end

local function pick_worktree(rows, here)
  local best
  for _, row in ipairs(rows) do
    local usable = row.from and row.from ~= "" and not row.error
    if usable and inside(real(row.path), here) then
      if not best or #row.path > #best.path then
        best = row
      end
    end
  end
  if best then
    return best
  end
  for _, row in ipairs(rows) do
    if row.from and row.from ~= "" and not row.error then
      return row
    end
  end
end

function M.diff(scope)
  local id = session()
  if not id then
    return
  end
  if vim.fn.exists(":DiffviewOpen") ~= 2 then
    fail("diffview.nvim is not installed")
    return
  end
  local args = { "review", "scope", "--session", id }
  if scope and scope ~= "" then
    vim.list_extend(args, { "--scope", scope })
  end
  local out, err = run(args)
  if not out then
    fail(err)
    return
  end
  local decoded, rows = pcall(vim.json.decode, out)
  if not decoded or type(rows) ~= "table" then
    fail("could not read the review scope: " .. out)
    return
  end
  local name = vim.api.nvim_buf_get_name(0)
  local here = name ~= "" and real(vim.fs.dirname(name)) or real(vim.uv.cwd())
  local row = pick_worktree(rows, here)
  if not row then
    fail((rows[1] and rows[1].error) or "the session has no worktree to diff")
    return
  end
  vim.cmd({ cmd = "DiffviewOpen", args = { row.from, "--untracked-files=true", "-C" .. row.path } })
end

local function send_comment(id, file, line1, line2, body)
  local code = table.concat(vim.api.nvim_buf_get_lines(0, line1 - 1, line2, false), "\n")
  local out, err = run({
    "review", "comment", "--session", id, "--file", file,
    "--start", tostring(line1), "--end", tostring(line2), "--code", code, "--body", body,
  })
  if not out then
    fail(err)
    return
  end
  local answer = vim.trim(out)
  say(answer ~= "" and answer or "comment added")
end

function M.comment(line1, line2, body)
  local id = session()
  if not id then
    return
  end
  local name = vim.api.nvim_buf_get_name(0)
  if name == "" then
    fail("this buffer has no file to comment on")
    return
  end
  local file = real(name)
  line1, line2 = math.min(line1, line2), math.max(line1, line2)
  if body and vim.trim(body) ~= "" then
    send_comment(id, file, line1, line2, body)
    return
  end
  local buf = vim.api.nvim_get_current_buf()
  vim.ui.input({ prompt = ("Comment on lines %d-%d: "):format(line1, line2) }, function(input)
    if not input or vim.trim(input) == "" then
      return
    end
    vim.api.nvim_buf_call(buf, function()
      send_comment(id, file, line1, line2, input)
    end)
  end)
end

function M.navigate(dir)
  local nav = navigation[dir]
  local before = vim.api.nvim_get_current_win()
  pcall(vim.cmd, "wincmd " .. dir)
  if vim.api.nvim_get_current_win() ~= before or (vim.env.TMUX or "") == "" then
    return
  end
  local edge = "#{pane_at_" .. nav.edge .. "}"
  pcall(function()
    vim.system({ M.config.tmux, "if", "-F", edge, "", "select-pane -" .. nav.pane }):wait(2000)
  end)
end

local function map_navigation()
  for dir, nav in pairs(navigation) do
    vim.keymap.set("n", nav.key, function()
      M.navigate(dir)
    end, { silent = true, desc = "crew: move between splits and tmux panes" })
    vim.keymap.set("t", nav.key, function()
      vim.cmd.stopinsert()
      M.navigate(dir)
    end, { silent = true, desc = "crew: move between splits and tmux panes" })
  end
  mapped = true
end

local function unmap_navigation()
  for _, nav in pairs(navigation) do
    pcall(vim.keymap.del, "n", nav.key)
    pcall(vim.keymap.del, "t", nav.key)
  end
  mapped = false
end

function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", defaults, opts or {})
  if M.config.navigate then
    map_navigation()
  elseif mapped then
    unmap_navigation()
  end
end

return M
