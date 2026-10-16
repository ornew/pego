-- Drives `pego lsp` from Neovim 0.11 or later, configured as in the editor guide. Each edit is
-- made with an ordinary editor command; the didChange notifications Neovim sends are recorded, and
-- the server's text is checked by comparing semantic tokens of the edited document with those of a
-- freshly opened copy of the buffer.
--
-- Usage: PEGO=<pego binary> RESPONSIVE=0|1 nvim --headless --clean -l neovim_client.lua <empty dir>
local dir = _G.arg[1]
local responsive = os.getenv("RESPONSIVE") == "1"
local function say(s) io.stdout:write(s .. "\n") end
local function fail(msg)
  say("FAIL " .. msg)
  os.exit(1)
end

vim.filetype.add({ extension = { pego = "pego" } })
local capabilities = vim.lsp.protocol.make_client_capabilities()
capabilities.experimental = { pego = { responsiveAnalysis = responsive } }
vim.lsp.config("pego", {
  cmd = { os.getenv("PEGO"), "lsp" },
  filetypes = { "pego" },
  root_markers = { ".git" },
  capabilities = capabilities,
})
vim.lsp.enable("pego")

local lines = {}
for i = 1, 200 do
  lines[#lines + 1] = string.format('rule r%d = "a%d" / r%d', i, i, i + 1)
end
lines[#lines + 1] = 'rule r201 = "z"'
lines[#lines + 1] = 'rule main = r1 ("é😀" / [0-9])* $$'
vim.fn.mkdir(dir .. "/.git", "p")
local path = dir .. "/edit.pego"
vim.fn.writefile(lines, path)
vim.cmd.edit(path)
local buf = vim.api.nvim_get_current_buf()

if not vim.wait(10000, function()
  local c = vim.lsp.get_clients({ bufnr = buf, name = "pego" })[1]
  return c ~= nil and c.initialized
end, 20) then
  fail("client did not attach")
end
local client = vim.lsp.get_clients({ bufnr = buf, name = "pego" })[1]

local sent = {}
local notify = client.rpc.notify
client.rpc.notify = function(method, params)
  if method == "textDocument/didChange" then
    sent[#sent + 1] = vim.deepcopy(params.contentChanges)
  end
  return notify(method, params)
end

-- order classifies the ranges of one notification.
local function order(changes)
  if #changes < 2 then
    return "single"
  end
  local up, down = true, true
  for i = 2, #changes do
    local a, b = changes[i - 1].range, changes[i].range
    if not a or not b then
      return "full"
    end
    local after = b.start.line > a.start.line or (b.start.line == a.start.line and b.start.character > a.start.character)
    local before = b.start.line < a.start.line or (b.start.line == a.start.line and b.start.character < a.start.character)
    up = up and not after
    down = down and not before
  end
  return up and "bottom-up" or down and "top-down" or "mixed"
end

local function tokens(b)
  local res = client:request_sync("textDocument/semanticTokens/full", { textDocument = { uri = vim.uri_from_bufnr(b) } }, 10000, b)
  if not res or res.err or not res.result then
    fail("semantic tokens: " .. vim.inspect(res))
  end
  return res.result.data
end

local function settle()
  vim.wait(400, function() return false end, 20)
end

local function check(name, edit)
  sent = {}
  edit()
  settle()
  local changes, kinds = 0, {}
  for _, cs in ipairs(sent) do
    changes = changes + #cs
    local kind = order(cs)
    if kind ~= "single" then
      kinds[kind] = (kinds[kind] or 0) + 1
    end
  end
  local multi = {}
  for kind, n in pairs(kinds) do
    multi[#multi + 1] = kind .. "=" .. n
  end
  table.sort(multi)
  local fresh = string.format("%s/fresh-%s.pego", dir, name)
  vim.fn.writefile(vim.api.nvim_buf_get_lines(buf, 0, -1, false), fresh)
  local fb = vim.fn.bufadd(fresh)
  vim.fn.bufload(fb)
  vim.bo[fb].filetype = "pego"
  vim.lsp.buf_attach_client(fb, client.id)
  local same = vim.deep_equal(tokens(buf), tokens(fb))
  vim.lsp.buf_detach_client(fb, client.id)
  say(string.format("%-16s notifications=%d changes=%d multi-change=%s server-text=%s", name, #sent, changes,
    #multi > 0 and table.concat(multi, ",") or "none", same and "match" or "MISMATCH"))
  if #sent == 0 then
    fail(name .. " sent no change")
  end
  if not same then
    fail(name .. " server text diverged")
  end
end

local function normal(keys)
  vim.cmd("silent execute \"normal! " .. keys .. "\"")
end
local function at(line, col)
  vim.api.nvim_win_set_cursor(0, { line, col or 0 })
end

check("insert-char", function() at(5, 4); normal("ix") end)
check("substitute", function() vim.cmd([[silent 10,40s/^/\/\/ /]]) end)
check("global-normal", function() vim.cmd([[silent g/r1[0-9][0-9] /normal! A // tail]]) end)
check("block-insert", function() at(50); normal("\\<C-v>9jI// \\<Esc>") end)
check("join-lines", function() at(60); normal("5J") end)
check("delete-lines", function() at(70); normal("10dd") end)
-- A script makes no undo breaks, so this undoes every edit above in one step.
check("undo", function() normal("u") end)
check("redo", function() normal("\\<C-r>") end)
check("unicode", function() at(vim.api.nvim_buf_line_count(buf)); normal("$F😀a😀é") end)
check("set-lines", function() vim.api.nvim_buf_set_lines(buf, 100, 100, false, { 'rule pasted1 = "p"', 'rule pasted2 = pasted1' }) end)
check("typing", function() at(120); for i = 1, 30 do normal("A" .. (i % 10)) end end)
check("macro", function() at(130); vim.fn.setreg("q", "I// \27j"); normal("20@q") end)

local uri = vim.uri_from_bufnr(buf)
-- Hover the reference to r1 in the main rule.
local main = vim.fn.search([[^rule main = r1]], "w") - 1
local hover = client:request_sync("textDocument/hover", { textDocument = { uri = uri }, position = { line = main, character = 12 } }, 10000, buf)
if not hover or hover.err or not hover.result then
  fail("hover: " .. vim.inspect(hover))
end
local format = client:request_sync("textDocument/formatting", { textDocument = { uri = uri }, options = { tabSize = 4, insertSpaces = true } }, 10000, buf)
if not format or format.err then
  fail("formatting: " .. vim.inspect(format))
end
say("PASS")
client:stop()
vim.wait(2000, function() return client:is_stopped() end, 20)
os.exit(0)
