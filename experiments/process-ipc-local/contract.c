/* Compile/link contract only. Never execute this image as a qualification run. */
extern int lo_ipc_local_fixture(int variant);

int main(void) {
    return lo_ipc_local_fixture(0);
}
