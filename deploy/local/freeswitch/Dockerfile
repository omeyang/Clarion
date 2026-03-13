# FreeSWITCH + mod_audio_fork 构建
#
# 基于 Debian，从 SignalWire 仓库安装 FreeSWITCH，
# 然后从源码编译 mod_audio_fork（drachtio 项目）。

FROM debian:bookworm-slim AS builder

# 安装 FreeSWITCH 和编译依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
    gnupg2 wget ca-certificates lsb-release git \
    build-essential cmake pkg-config \
    libssl-dev libcurl4-openssl-dev \
    libspeex-dev libspeexdsp-dev \
    liblua5.3-dev \
    uuid-dev \
    && rm -rf /var/lib/apt/lists/*

# 添加 SignalWire 仓库并安装 FreeSWITCH
# 注意：如果 SignalWire 需要 token，可以改用下方的源码编译方式
RUN wget --quiet -O /usr/share/keyrings/signalwire-freeswitch-repo.gpg \
    https://freeswitch.signalwire.com/repo/deb/debian-release/signalwire-freeswitch-repo.gpg 2>/dev/null || true

# 如果包安装失败，使用备用方案：从 FreeSWITCH 源码编译最小集
# 这里采用更可靠的方式：直接从 GitHub 编译 FreeSWITCH 最小集
RUN git clone --depth 1 -b v1.10.12 https://github.com/signalwire/freeswitch.git /usr/src/freeswitch

WORKDIR /usr/src/freeswitch

# 安装 FreeSWITCH 编译依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
    autoconf automake libtool libtool-bin \
    libsqlite3-dev libpcre3-dev \
    libsndfile1-dev libedit-dev \
    yasm nasm libopus-dev \
    python3-dev swig \
    && rm -rf /var/lib/apt/lists/*

# 配置最小模块集（仅构建回环测试所需模块）
RUN cp build/modules.conf.in modules.conf \
    && sed -i 's/.*mod_signalwire.*/#mod_signalwire/' modules.conf

RUN ./bootstrap.sh -j \
    && ./configure --prefix=/usr/local/freeswitch \
       --disable-debug \
       --disable-libyuv \
       --disable-libvpx \
    && make -j$(nproc) \
    && make install

# 编译 mod_audio_fork
RUN git clone --depth 1 https://github.com/drachtio/freeswitch-modules.git /tmp/fs-modules

WORKDIR /tmp/fs-modules/modules/mod_audio_fork

RUN export PKG_CONFIG_PATH=/usr/local/freeswitch/lib/pkgconfig:$PKG_CONFIG_PATH \
    && if [ -f CMakeLists.txt ]; then \
         mkdir -p build && cd build \
         && cmake .. -DCMAKE_INSTALL_PREFIX=/usr/local/freeswitch \
         && make -j$(nproc) \
         && cp mod_audio_fork.so /usr/local/freeswitch/mod/; \
       elif [ -f Makefile ]; then \
         make -j$(nproc) \
         && cp .libs/mod_audio_fork.so /usr/local/freeswitch/mod/ 2>/dev/null \
         || cp mod_audio_fork.so /usr/local/freeswitch/mod/; \
       fi

# --- 运行时镜像 ---
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libssl3 libcurl4 libsqlite3-0 libpcre3 \
    libsndfile1 libedit2 libspeex1 libspeexdsp1 \
    libopus0 liblua5.3-0 ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 复制 FreeSWITCH 安装
COPY --from=builder /usr/local/freeswitch /usr/local/freeswitch

# 创建录音目录
RUN mkdir -p /recordings /tmp/test-audio

# 复制配置文件
COPY conf/ /usr/local/freeswitch/conf/

# ESL + SIP 端口
EXPOSE 8021 5060/tcp 5060/udp

ENV PATH="/usr/local/freeswitch/bin:$PATH"

CMD ["freeswitch", "-nonat", "-nf"]
