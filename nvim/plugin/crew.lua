if vim.g.loaded_crew then
  return
end
vim.g.loaded_crew = 1

vim.api.nvim_create_user_command("CrewDiff", function(o)
  require("crew").diff(o.args)
end, {
  nargs = "?",
  complete = function()
    return { "last_turn", "uncommitted", "branch" }
  end,
  desc = "crew: open the session's review scope in diffview",
})

vim.api.nvim_create_user_command("CrewComment", function(o)
  require("crew").comment(o.line1, o.line2, o.args)
end, {
  nargs = "*",
  range = true,
  desc = "crew: add the lines as a draft review comment",
})
