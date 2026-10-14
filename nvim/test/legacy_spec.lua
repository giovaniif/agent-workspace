vim.opt.rtp:prepend(assert(vim.env.CREW_NVIM_PLUGIN, "CREW_NVIM_PLUGIN is not set"))

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

test("the old module name loads the crew plugin", function()
  assert(require("agentws") == require("crew"), "require('agentws') is not the crew module")
end)

test("a session id in the old variable is still found", function()
  vim.env.CREW_SESSION = nil
  vim.env.AGENTWS_SESSION = "old-session"
  assert(require("crew.legacy").session() == "old-session", "old variable not read")
end)

if failed > 0 then
  print(("%d spec(s) failed"):format(failed))
  vim.cmd("cquit 1")
end
print("all specs passed")
