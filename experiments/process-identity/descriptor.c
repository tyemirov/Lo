#include <unistd.h>

// Compile and link only. An SDK declaration must supply the API contract.
int main(void) {
    char *arguments[] = {"fixture", 0};
    char *environment[] = {0};
    return fexecve(-1, arguments, environment);
}
