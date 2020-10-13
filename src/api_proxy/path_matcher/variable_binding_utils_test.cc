// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include "src/api_proxy/path_matcher/variable_binding_utils.h"

#include "gtest/gtest.h"

namespace espv2 {
namespace api_proxy {
namespace path_matcher {

TEST(VariableBindingsToQueryParameters, Test) {
  EXPECT_EQ(VariableBindingsToQueryParameters(/*variable_bindings=*/{}), "");
  EXPECT_EQ(VariableBindingsToQueryParameters(
                /*variable_bindings=*/
                {
                    {{"id"}, "42"},
                }),
            "id=42");
  EXPECT_EQ(VariableBindingsToQueryParameters(
                /*variable_bindings=*/
                {
                    {{"foo_bar"}, "42"},
                }),
            "foo_bar=42");
  EXPECT_EQ(VariableBindingsToQueryParameters(
                /*variable_bindings=*/
                {
                    {{"id"}, "42"},
                    {{"foo", "bar", "baz"}, "value"},
                    {{"x", "y"}, "abc"},
                }),
            "id=42&foo.bar.baz=value&x.y=abc");
}
}  // namespace path_matcher
}  // namespace api_proxy
}  // namespace espv2
