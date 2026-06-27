#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <lua.h>
#include <lualib.h>
#include <lauxlib.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	lua_State *L = luaL_newstate();
	if (!L) {
		return 0;
	}
	luaL_openlibs(L);
	(void)luaL_loadbuffer(L, buf, n, "stdin");
	lua_close(L);
	return 0;
}
